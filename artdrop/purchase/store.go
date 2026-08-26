package purchase

// Store defines what purchase needs from the database.
type Store interface {
	// CreatePurchaseCharge persists a purchase charge audit record.
	CreatePurchaseCharge(charge *PurchaseCharge) error
}
