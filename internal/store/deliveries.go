package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"umirhack/internal/domain"
)

// ErrDeliveryConflict — тот же id поставки, другое содержимое.
var ErrDeliveryConflict = errors.New("store: delivery id reused with different content")

// ErrUnitMismatch — единица продукта в БД не совпала с единицей позиции.
var ErrUnitMismatch = errors.New("store: product unit mismatch")

// ErrNoShelfLife — у позиции нет срока. Срок годности — обязательная часть поставки (его даёт поставщик):
// срока по умолчанию из карточки продукта для поставки нет (FS-160, ответ эксперта «Флагмана»).
var ErrNoShelfLife = errors.New("store: no shelf life for item")

// NoShelfLifeError указывает позицию (с 0); errors.Is(err, ErrNoShelfLife) истинно.
type NoShelfLifeError struct {
	Index     int
	ProductID string
}

func (e *NoShelfLifeError) Error() string {
	return fmt.Sprintf("store: item %d: product %s has no expiry in the delivery", e.Index, e.ProductID)
}
func (e *NoShelfLifeError) Is(target error) bool { return target == ErrNoShelfLife }

// ErrEventOutsideWindow — occurred_at вне окна [now − LateEvents, now]; errors.Is истинно для *EventWindowError.
var ErrEventOutsideWindow = errors.New("store: occurred_at outside the late-events window")

// EventWindowError несёт границы окна для подсказки в ответе API.
type EventWindowError struct {
	OccurredAt, From, To time.Time
}

func (e *EventWindowError) Error() string {
	return fmt.Sprintf("store: occurred_at %s outside [%s, %s]", e.OccurredAt.Format(time.RFC3339),
		e.From.Format(time.RFC3339), e.To.Format(time.RFC3339))
}
func (e *EventWindowError) Is(target error) bool { return target == ErrEventOutsideWindow }

// checkOccurredAt проверяет now − late ≤ occurred ≤ now (now прочитан под блокировками).
func checkOccurredAt(occurred, now time.Time, late time.Duration) error {
	if occurred.After(now) || occurred.Before(now.Add(-late)) {
		return &EventWindowError{OccurredAt: occurred, From: now.Add(-late), To: now}
	}
	return nil
}

// Warning кодов предупреждений поставки.
const WarnExpiredOnArrival = "expired_on_arrival"

// DeliveryItem — нормализованная позиция: количество уже в базовых единицах.
// Ровно одно из ShelfLifeHours (>0) и ExpiresOn (YYYY-MM-DD); оба пусты — NoShelfLifeError (FS-160).
type DeliveryItem struct {
	ProductID      string
	Name           string
	Unit           domain.Unit // базовая единица продукта
	Qty            int64
	ShelfLifeHours int
	ExpiresOn      string
}

// DeliveryInput — поставка после разбора и проверки на границе API.
type DeliveryInput struct {
	ID        string
	Number    string
	CreatedAt time.Time
	// OccurredAt — когда поставка пришла фактически (необязательно); входит в хэш. Задано —
	// received_at = OccurredAt и срок считается от него (2.4а).
	OccurredAt *time.Time
	Items      []DeliveryItem
}

// QtyJSON — количество в ответах: {"qty": 1500, "unit": "g"}.
type QtyJSON struct {
	Qty  int64       `json:"qty"`
	Unit domain.Unit `json:"unit"`
}

// BatchJSON — партия в ответе приёма поставки.
type BatchJSON struct {
	ID         int64     `json:"id"`
	LineNo     int       `json:"line_no"`
	ProductID  string    `json:"product_id"`
	Quantity   QtyJSON   `json:"quantity"`
	ReceivedAt time.Time `json:"received_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Status     string    `json:"status"`
}

// WarningJSON — предупреждение по позиции.
type WarningJSON struct {
	LineNo  int    `json:"line_no"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// DeliveryResponse — тело ответа и сохранённый ответ поставки.
type DeliveryResponse struct {
	ID         string        `json:"id"`
	Number     string        `json:"number"`
	CreatedAt  time.Time     `json:"created_at"`
	ReceivedAt time.Time     `json:"received_at"`
	Replayed   bool          `json:"replayed"`
	Batches    []BatchJSON   `json:"batches"`
	Warnings   []WarningJSON `json:"warnings"`
}

// BodyHash — хэш нормализованного содержимого: id, number, created_at и позиции
// (product_id, количество, срок в исходном виде), отсортированные по этой тройке; name не входит.
func (in DeliveryInput) BodyHash() []byte {
	type line struct {
		P string `json:"p"`
		Q int64  `json:"q"`
		S string `json:"s"`
	}
	occ := ""
	if in.OccurredAt != nil {
		occ = in.OccurredAt.UTC().Format(time.RFC3339Nano)
	}
	lines := make([]line, len(in.Items))
	for i, it := range in.Items {
		s := "d:" + it.ExpiresOn
		switch {
		case it.ExpiresOn != "":
		case it.ShelfLifeHours > 0:
			s = "h:" + strconv.Itoa(it.ShelfLifeHours)
		default:
			s = "p:" // позиция без срока: так хэшировались поставки до FS-160 — хэш не меняем, повтор сверяется по-старому
		}
		lines[i] = line{it.ProductID, it.Qty, s}
	}
	sort.Slice(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		if a.P != b.P {
			return a.P < b.P
		}
		if a.Q != b.Q {
			return a.Q < b.Q
		}
		return a.S < b.S
	})
	raw, _ := json.Marshal(struct {
		ID        string `json:"id"`
		Number    string `json:"number"`
		CreatedAt string `json:"created_at"`
		// Пусто для поставок без occurred_at: прежние хэши не меняются.
		OccurredAt string `json:"occurred_at,omitempty"`
		Lines      []line `json:"lines"`
	}{in.ID, in.Number, in.CreatedAt.UTC().Format(time.RFC3339Nano), occ, lines})
	sum := sha256.Sum256(raw)
	return sum[:]
}

// ProductUnits возвращает базовые единицы известных продуктов; неизвестные в карту не попадают.
func (d *DB) ProductUnits(ctx context.Context, ids []string) (map[string]domain.Unit, error) {
	rows, err := d.Pool.Query(ctx, `SELECT id, unit FROM products WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("store: product units: %w", err)
	}
	defer rows.Close()
	out := map[string]domain.Unit{}
	for rows.Next() {
		var id, u string
		if err := rows.Scan(&id, &u); err != nil {
			return nil, fmt.Errorf("store: product units: %w", err)
		}
		out[id] = domain.Unit(u)
	}
	return out, rows.Err()
}

// ReceiveDelivery принимает поставку одной транзакцией в порядке 2.1: вставка поставки →
// автосоздание продуктов → блокировки → received_at = now() → партии и движения → ответ.
// now вызывается только после получения блокировок. loc — часовой пояс ресторана для expires_on.
func (d *DB) ReceiveDelivery(ctx context.Context, in DeliveryInput, now func() time.Time, loc *time.Location) (DeliveryResponse, error) {
	hash := in.BodyHash()
	var resp DeliveryResponse
	err := d.InTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`INSERT INTO deliveries (id, number, created_at, received_at, body_hash, response)
			 VALUES ($1, $2, $3, $3, $4, '{}') ON CONFLICT (id) DO NOTHING`,
			in.ID, in.Number, in.CreatedAt, hash)
		if err != nil {
			return fmt.Errorf("store: insert delivery: %w", err)
		}
		if tag.RowsAffected() == 0 {
			var stored []byte
			var raw []byte
			if err := tx.QueryRow(ctx, `SELECT body_hash, response FROM deliveries WHERE id = $1`, in.ID).Scan(&stored, &raw); err != nil {
				return fmt.Errorf("store: read delivery: %w", err)
			}
			if string(stored) != string(hash) {
				return ErrDeliveryConflict
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return fmt.Errorf("store: stored delivery response: %w", err)
			}
			resp.Replayed = true
			return nil
		}

		// Автосоздание — в порядке отсортированных уникальных id, иначе вставки новых продуктов
		// в противоположном порядке дают deadlock. Имя и единица — из первой позиции с этим id.
		first := map[string]DeliveryItem{}
		ids := make([]string, 0, len(in.Items))
		for _, it := range in.Items {
			if _, ok := first[it.ProductID]; !ok {
				first[it.ProductID] = it
				ids = append(ids, it.ProductID)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			it := first[id]
			name := it.Name
			if name == "" {
				name = it.ProductID
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO products (id, name, unit, auto_created) VALUES ($1, $2, $3, true) ON CONFLICT (id) DO NOTHING`,
				it.ProductID, name, string(it.Unit)); err != nil {
				return fmt.Errorf("store: auto-create product: %w", err)
			}
		}
		if err := LockProducts(ctx, tx, ids); err != nil {
			return err
		}
		units := map[string]domain.Unit{}
		for _, id := range ids {
			var u string
			if err := tx.QueryRow(ctx, `SELECT unit FROM products WHERE id = $1`, id).Scan(&u); err != nil {
				return fmt.Errorf("store: product unit: %w", err)
			}
			units[id] = domain.Unit(u)
		}
		for i, it := range in.Items {
			if units[it.ProductID] != it.Unit {
				return fmt.Errorf("%w: %s is %s", ErrUnitMismatch, it.ProductID, units[it.ProductID])
			}
			if it.ExpiresOn == "" && it.ShelfLifeHours == 0 {
				return &NoShelfLifeError{Index: i, ProductID: it.ProductID}
			}
		}

		received := now().UTC()
		cur := received
		if in.OccurredAt != nil {
			if err := checkOccurredAt(in.OccurredAt.UTC(), cur, d.LateEvents); err != nil {
				return err
			}
			received = in.OccurredAt.UTC()
		}
		resp = DeliveryResponse{ID: in.ID, Number: in.Number, CreatedAt: in.CreatedAt, ReceivedAt: received,
			Batches: []BatchJSON{}, Warnings: []WarningJSON{}}
		for i, it := range in.Items {
			line := i + 1
			source := ExpirySourceDelivery
			expires := received.Add(time.Duration(it.ShelfLifeHours) * time.Hour)
			if it.ExpiresOn != "" {
				day, err := time.ParseInLocation("2006-01-02", it.ExpiresOn, loc)
				if err != nil {
					return fmt.Errorf("store: expires_on %q: %w", it.ExpiresOn, err)
				}
				expires = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc).UTC()
			}
			expired := !expires.After(received)
			remaining, status := it.Qty, "ok"
			if !expires.After(cur) {
				status = "expired" // истёк, но ещё не списан: «дворник» ждёт окно событий задним числом
			}
			var writtenOff *time.Time
			if expired {
				remaining, status, writtenOff = 0, "expired", &received
			}
			var bid int64
			if err := tx.QueryRow(ctx,
				`INSERT INTO batches (delivery_id, line_no, product_id, quantity, remaining, received_at, expires_at, written_off_at, expiry_source)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
				in.ID, line, it.ProductID, it.Qty, remaining, received, expires, writtenOff, source).Scan(&bid); err != nil {
				return fmt.Errorf("store: insert batch: %w", err)
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO movements (batch_id, product_id, kind, quantity, at) VALUES ($1, $2, 'receipt', $3, $4)`,
				bid, it.ProductID, it.Qty, received); err != nil {
				return fmt.Errorf("store: insert receipt: %w", err)
			}
			if expired {
				if _, err := tx.Exec(ctx,
					`INSERT INTO movements (batch_id, product_id, kind, reason, quantity, at) VALUES ($1, $2, 'writeoff', 'expired', $3, $4)`,
					bid, it.ProductID, it.Qty, received); err != nil {
					return fmt.Errorf("store: insert writeoff: %w", err)
				}
				resp.Warnings = append(resp.Warnings, WarningJSON{LineNo: line, Code: WarnExpiredOnArrival,
					Message: fmt.Sprintf("batch expired before receipt; written off at stand time %s", received.Format(time.RFC3339))})
			}
			resp.Batches = append(resp.Batches, BatchJSON{ID: bid, LineNo: line, ProductID: it.ProductID,
				Quantity: QtyJSON{it.Qty, it.Unit}, ReceivedAt: received, ExpiresAt: expires, Status: status})
		}
		raw, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("store: marshal response: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE deliveries SET received_at = $2, response = $3 WHERE id = $1`, in.ID, received, raw); err != nil {
			return fmt.Errorf("store: save response: %w", err)
		}
		return nil
	})
	return resp, err
}

// ListDeliveries — история поставок, новые сверху.
func (d *DB) ListDeliveries(ctx context.Context) ([]DeliveryResponse, error) {
	rows, err := d.Pool.Query(ctx, `SELECT response FROM deliveries ORDER BY received_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list deliveries: %w", err)
	}
	defer rows.Close()
	out := []DeliveryResponse{}
	for rows.Next() {
		var raw []byte
		var r DeliveryResponse
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("store: list deliveries: %w", err)
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("store: list deliveries: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ErrNotFound — записи нет.
var ErrNotFound = errors.New("store: not found")

// GetDelivery возвращает сохранённый ответ поставки.
func (d *DB) GetDelivery(ctx context.Context, id string) (DeliveryResponse, error) {
	var raw []byte
	err := d.Pool.QueryRow(ctx, `SELECT response FROM deliveries WHERE id = $1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeliveryResponse{}, ErrNotFound
	}
	if err != nil {
		return DeliveryResponse{}, fmt.Errorf("store: get delivery: %w", err)
	}
	var r DeliveryResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return DeliveryResponse{}, fmt.Errorf("store: get delivery: %w", err)
	}
	return r, nil
}
