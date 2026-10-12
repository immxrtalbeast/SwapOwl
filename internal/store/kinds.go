package store

// Виды и причины движений (movements.kind, movements.reason) — единственное место, где они
// перечислены (.ai/solution.md, 8.2). В БД на них нет CHECK и enum: новые значения добавляются
// без миграции таблицы. Новый код берёт значения отсюда; старые SQL-литералы ('sale', 'receipt',
// 'writeoff', 'expired') переводить на константы — отдельным тикетом.
//
// Знак движения задаёт вид: receipt и return увеличивают остаток партии, sale, writeoff
// и adjustment уменьшают; quantity всегда > 0. transfer_in увеличивает остаток (у дочерней
// партии он единственный приход), transfer_out уменьшает. Баланс партии:
// receipt + transfer_in + return − sale − writeoff − adjustment − transfer_out = remaining.
const (
	KindReceipt    = "receipt"
	KindSale       = "sale"
	KindWriteoff   = "writeoff"
	KindAdjustment = "adjustment"
	KindReturn     = "return"
	// KindTransferOut и KindTransferIn — перенос остатка между партиями одной операцией
	// (вскрытие упаковки, docs/opened.md): out уменьшает исходную партию, in заводит дочернюю.
	KindTransferOut = "transfer_out"
	KindTransferIn  = "transfer_in"
)

// Причины движений.
const (
	ReasonExpired           = "expired"            // writeoff: истёк срок (at = expires_at партии)
	ReasonSpoilage          = "spoilage"           // writeoff: порча
	ReasonDamage            = "damage"             // writeoff: повреждение, брак
	ReasonInventoryShortage = "inventory_shortage" // adjustment: недостача по инвентаризации
	ReasonReturnExpired     = "return_expired"     // adjustment: возврат в уже просроченную партию
	ReasonInventorySurplus  = "inventory_surplus"  // receipt: излишек по инвентаризации
	ReasonOpened            = "opened"             // transfer_out/transfer_in: вскрытие упаковки
)
