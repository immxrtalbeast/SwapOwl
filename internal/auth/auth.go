// Package auth — роли, сессии, ключи интеграции, смена пароля и ограничитель попыток входа
// (.ai/solution.md, 2.9). Пакет не знает о HTTP и о конкретной БД: хранилище приходит через Repo.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost — стоимость bcrypt по 2.9.
const BcryptCost = 12

// Role — роль пользователя или ключа.
type Role string

// Роли по возрастанию прав.
const (
	Viewer   Role = "viewer"
	Operator Role = "operator"
	Admin    Role = "admin"
)

func (r Role) rank() int {
	switch r {
	case Viewer:
		return 1
	case Operator:
		return 2
	case Admin:
		return 3
	}
	return 0
}

// Valid — известная роль.
func (r Role) Valid() bool { return r.rank() > 0 }

// Allows — хватает ли роли r для требования min.
func (r Role) Allows(min Role) bool { return r.rank() >= min.rank() && r.rank() > 0 }

// Ошибки пакета; API переводит их в коды из 7.4.
var (
	ErrNoCredentials      = errors.New("auth: no credentials")
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrTooManyAttempts    = errors.New("auth: too many attempts")
	ErrNotFound           = errors.New("auth: not found")
	ErrExists             = errors.New("auth: already exists")
	ErrLastAdmin          = errors.New("auth: cannot delete the last admin")
)

// User — пользователь. Хэш пароля наружу не уходит (в JSON его нет).
type User struct {
	ID           int64     `json:"id"`
	Login        string    `json:"login"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	PasswordHash string    `json:"-"`
}

// APIKey — ключ интеграции; сам ключ хранится только как SHA-256.
type APIKey struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// Kind — чем подтверждена личность.
type Kind string

// Виды принципала.
const (
	KindAnonymous Kind = "anonymous" // AUTH_REQUIRED=false, без учётных данных
	KindSession   Kind = "session"
	KindAPIKey    Kind = "api_key"
)

// Principal — кто делает запрос.
type Principal struct {
	Kind   Kind
	Role   Role
	UserID int64 // для сессии
	KeyID  int64 // для ключа
	Login  string
}

// Repo — хранилище; реализует internal/store.
type Repo interface {
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, login, passwordHash string, role Role) (User, error)
	UserByLogin(ctx context.Context, login string) (User, error)
	UserByID(ctx context.Context, id int64) (User, error)
	ListUsers(ctx context.Context) ([]User, error)
	// DeleteUser возвращает ErrNotFound или ErrLastAdmin.
	DeleteUser(ctx context.Context, id int64) error

	CreateSession(ctx context.Context, tokenHash []byte, userID int64, expiresAt time.Time) error
	// SessionUser возвращает владельца непросроченной сессии или ErrNotFound.
	SessionUser(ctx context.Context, tokenHash []byte, now time.Time) (User, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error
	// ChangePassword атомарно меняет хэш пароля и удаляет все сессии пользователя, кроме keepTokenHash.
	ChangePassword(ctx context.Context, userID int64, passwordHash string, keepTokenHash []byte) error

	// TakeLoginAttempt атомарно проверяет лимит по ключу и занимает попытку: false — за окно
	// window уже было max попыток (429). Счётчик живёт в БД и общий для всех экземпляров и рестартов.
	TakeLoginAttempt(ctx context.Context, key string, now time.Time, window time.Duration, max int) (bool, error)
	// ClearLoginAttempts сбрасывает счётчик ключа после успешного входа.
	ClearLoginAttempts(ctx context.Context, key string) error

	CreateAPIKey(ctx context.Context, name string, keyHash []byte, role Role) (APIKey, error)
	APIKeyByHash(ctx context.Context, keyHash []byte) (APIKey, error)
	ListAPIKeys(ctx context.Context) ([]APIKey, error)
	DeleteAPIKey(ctx context.Context, id int64) error
}

// Options — настройки сервиса.
type Options struct {
	Required     bool          // AUTH_REQUIRED
	SessionTTL   time.Duration // SESSION_TTL_HOURS
	SecureCookie bool          // cookie с Secure (false только для локального HTTP)
	Cost         int           // стоимость bcrypt; 0 — BcryptCost (меньше — только в тестах)
	Now          func() time.Time
}

// Service — вход, сессии и ключи.
type Service struct {
	Repo Repo
	Options
}

// Лимит попыток входа по одному логину (2.9): больше 5 за 15 минут — отказ.
const (
	MaxLoginAttempts = 5
	LoginWindow      = 15 * time.Minute
)

// New создаёт сервис. Часы сессий — реальные: ускоренное время стенда на сроки входа не влияет.
func New(repo Repo, opt Options) *Service {
	if opt.Cost == 0 {
		opt.Cost = BcryptCost
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.SessionTTL <= 0 {
		opt.SessionTTL = 12 * time.Hour
	}
	return &Service{Repo: repo, Options: opt}
}

// HashPassword — bcrypt.
func HashPassword(password string, cost int) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(h), nil
}

// NewToken возвращает случайный токен из 32 байт (base64url без заполнения).
func NewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("auth: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// HashToken — SHA-256 токена, то, что лежит в БД.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// keyPrefix отличает ключ интеграции от прочих токенов в чужих журналах и сканерах секретов.
const keyPrefix = "fsk_"

var (
	dummyOnce sync.Once
	dummyHash []byte
)

// burnPassword тратит столько же времени, сколько проверка настоящего пароля:
// неверный логин и неверный пароль не различаются ни ответом, ни задержкой.
func (s *Service) burnPassword(password string) {
	dummyOnce.Do(func() {
		dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy"), s.Cost)
	})
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
}

// Login проверяет пароль и открывает сессию. Возвращает токен (для cookie) и пользователя.
func (s *Service) Login(ctx context.Context, login, password string) (string, User, error) {
	if err := s.takeAttempt(ctx, login); err != nil {
		return "", User{}, err
	}
	u, err := s.Repo.UserByLogin(ctx, login)
	switch {
	case errors.Is(err, ErrNotFound):
		s.burnPassword(password)
		return "", User{}, ErrInvalidCredentials
	case err != nil:
		return "", User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", User{}, ErrInvalidCredentials
	}
	if err := s.Repo.ClearLoginAttempts(ctx, login); err != nil {
		return "", User{}, err
	}

	now := s.Now()
	if err := s.Repo.DeleteExpiredSessions(ctx, now); err != nil {
		return "", User{}, err
	}
	token, err := NewToken()
	if err != nil {
		return "", User{}, err
	}
	if err := s.Repo.CreateSession(ctx, HashToken(token), u.ID, now.Add(s.SessionTTL)); err != nil {
		return "", User{}, err
	}
	return token, u, nil
}

// takeAttempt занимает попытку по ключу; занятая попытка считается неудачной, пока вызывающий
// не сбросит счётчик после успеха — так параллельная пачка запросов не обходит лимит, пока идёт bcrypt.
func (s *Service) takeAttempt(ctx context.Context, key string) error {
	ok, err := s.Repo.TakeLoginAttempt(ctx, key, s.Now(), LoginWindow, MaxLoginAttempts)
	switch {
	case err != nil:
		return err
	case !ok:
		return ErrTooManyAttempts
	}
	return nil
}

// ChangePassword меняет пароль пользователя userID: проверяет старый, пишет bcrypt нового и
// удаляет все его сессии, кроме текущей (keepToken; пустой — удаляются все). Неверный старый
// пароль — ErrInvalidCredentials; больше 5 неверных за 15 минут — ErrTooManyAttempts.
// Длину и отличие нового пароля от старого проверяет вызывающий.
func (s *Service) ChangePassword(ctx context.Context, userID int64, keepToken, oldPassword, newPassword string) error {
	u, err := s.Repo.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	key := "password:" + u.Login // отдельный счётчик: подбор старого пароля не блокирует вход
	if err := s.takeAttempt(ctx, key); err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPassword)) != nil {
		return ErrInvalidCredentials
	}
	if err := s.Repo.ClearLoginAttempts(ctx, key); err != nil {
		return err
	}
	h, err := HashPassword(newPassword, s.Cost)
	if err != nil {
		return err
	}
	var keep []byte
	if keepToken != "" {
		keep = HashToken(keepToken)
	}
	return s.Repo.ChangePassword(ctx, userID, h, keep)
}

// Logout удаляет сессию; неизвестный токен — не ошибка.
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.Repo.DeleteSession(ctx, HashToken(token))
}

// Authenticate находит принципала по токену сессии и/или ключу из Bearer.
// Ничего не передано — ErrNoCredentials; передано, но недействительно — ErrInvalidCredentials.
func (s *Service) Authenticate(ctx context.Context, sessionToken, bearer string) (Principal, error) {
	if sessionToken == "" && bearer == "" {
		return Principal{}, ErrNoCredentials
	}
	if sessionToken != "" {
		u, err := s.Repo.SessionUser(ctx, HashToken(sessionToken), s.Now())
		switch {
		case err == nil:
			return Principal{Kind: KindSession, Role: u.Role, UserID: u.ID, Login: u.Login}, nil
		case !errors.Is(err, ErrNotFound):
			return Principal{}, err
		}
	}
	if bearer != "" {
		k, err := s.Repo.APIKeyByHash(ctx, HashToken(bearer))
		switch {
		case err == nil:
			return Principal{Kind: KindAPIKey, Role: k.Role, KeyID: k.ID}, nil
		case !errors.Is(err, ErrNotFound):
			return Principal{}, err
		}
	}
	return Principal{}, ErrInvalidCredentials
}

// Anonymous — принципал открытого стенда (AUTH_REQUIRED=false).
func Anonymous() Principal { return Principal{Kind: KindAnonymous, Role: Operator} }

// CreateUser создаёт пользователя. Пароль проверяет вызывающий (длина), здесь — только bcrypt.
func (s *Service) CreateUser(ctx context.Context, login, password string, role Role) (User, error) {
	if !role.Valid() {
		return User{}, fmt.Errorf("auth: unknown role %q", role)
	}
	h, err := HashPassword(password, s.Cost)
	if err != nil {
		return User{}, err
	}
	return s.Repo.CreateUser(ctx, login, h, role)
}

// Bootstrap создаёт первого admin, если пользователей нет, и чистит просроченные сессии
// (2.9: при старте). Пустые login/password — только очистка.
func (s *Service) Bootstrap(ctx context.Context, login, password string) error {
	if err := s.Repo.DeleteExpiredSessions(ctx, s.Now()); err != nil {
		return err
	}
	if login == "" || password == "" {
		return nil
	}
	n, err := s.Repo.CountUsers(ctx)
	if err != nil || n > 0 {
		return err
	}
	if _, err := s.CreateUser(ctx, login, password, Admin); err != nil && !errors.Is(err, ErrExists) {
		return err
	}
	return nil
}

// CreateAPIKey создаёт ключ и возвращает его открытый текст — единственный раз.
func (s *Service) CreateAPIKey(ctx context.Context, name string, role Role) (APIKey, string, error) {
	if role != Viewer && role != Operator {
		return APIKey{}, "", fmt.Errorf("auth: api key role must be viewer or operator, got %q", role)
	}
	t, err := NewToken()
	if err != nil {
		return APIKey{}, "", err
	}
	key := keyPrefix + t
	k, err := s.Repo.CreateAPIKey(ctx, name, HashToken(key), role)
	if err != nil {
		return APIKey{}, "", err
	}
	return k, key, nil
}

// TokenEqual — сравнение секретов за постоянное время.
func TokenEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
