package studio

// Store defines what studio needs from the database.
type Store interface {
	// CreateProductionCharge persists a charge audit record.
	CreateProductionCharge(charge *ProductionCharge) error
	// ListProductionChargesByUser returns the audit records for a user,
	// most recent first.
	ListProductionChargesByUser(userID string) ([]ProductionCharge, error)
}
