package studio

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// ErrChargeAlreadyRecorded is returned when a charge with the same Stripe
// payment intent has already been recorded. This is the idempotency guard at
// the audit layer: a double-click or a network retry must not create a
// duplicate charge record.
var ErrChargeAlreadyRecorded = errors.New("charge already recorded")

// Service lists all functionality provided by the studio service.
type Service interface {
	// RecordProductionCharge persists a charge audit record. It is
	// idempotent per Stripe payment intent: recording the same intent twice
	// returns ErrChargeAlreadyRecorded and does not create a duplicate row.
	RecordProductionCharge(in CreateProductionChargeInput) (*ProductionCharge, error)
	// ListProductionChargesByUser returns the audit records for a user.
	ListProductionChargesByUser(userID string) ([]ProductionCharge, error)
}

// ServiceImpl implements the studio Service.
type ServiceImpl struct {
	store Store
}

// NewService initiates a new studio service.
func NewService(store Store) Service {
	return &ServiceImpl{store: store}
}

// RecordProductionCharge persists a charge audit record, guarding against
// duplicates by Stripe payment intent.
func (s *ServiceImpl) RecordProductionCharge(in CreateProductionChargeInput) (*ProductionCharge, error) {
	if in.StripePaymentIntent == "" {
		return nil, fmt.Errorf("stripe payment intent is required")
	}
	if in.AmountCents <= 0 {
		return nil, fmt.Errorf("amount must be positive")
	}
	if in.UserID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	if in.Currency == "" {
		in.Currency = "usd"
	}

	charge := &ProductionCharge{
		UserID:              in.UserID,
		QuoteID:             in.QuoteID,
		AmountCents:         in.AmountCents,
		Currency:            in.Currency,
		StripePaymentIntent: in.StripePaymentIntent,
		PricingHash:         in.PricingHash,
		EngineVersion:       in.EngineVersion,
		Metadata:            in.Metadata,
	}

	if err := s.store.CreateProductionCharge(charge); err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrChargeAlreadyRecorded
		}
		return nil, fmt.Errorf("record production charge: %w", err)
	}

	return charge, nil
}

// ListProductionChargesByUser returns the audit records for a user.
func (s *ServiceImpl) ListProductionChargesByUser(userID string) ([]ProductionCharge, error) {
	if userID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	charges, err := s.store.ListProductionChargesByUser(userID)
	if err != nil {
		return nil, fmt.Errorf("list production charges: %w", err)
	}
	return charges, nil
}

// isDuplicateKeyError reports whether err is a unique-constraint violation.
// GORM translates driver errors to gorm.ErrDuplicatedKey on some drivers, but
// the SQLite driver used in tests surfaces the raw "UNIQUE constraint failed"
// message, so we match both.
func isDuplicateKeyError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate key")
}
