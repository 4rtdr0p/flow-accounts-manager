package studio

import (
	"context"
	"errors"
	"fmt"
	"strings"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"gorm.io/gorm"
)

// ErrChargeAlreadyRecorded is returned when a charge with the same Stripe
// payment intent has already been recorded. This is the idempotency guard at
// the audit layer: a double-click or a network retry must not create a
// duplicate charge record.
var ErrChargeAlreadyRecorded = errors.New("charge already recorded")

// ErrQuoteNotFound is returned when the requested studio quote does not exist.
var ErrQuoteNotFound = errors.New("studio quote not found")

// ErrPricingDisabled is returned when the pricing engine is not configured
// (Mongo disabled).
var ErrPricingDisabled = errors.New("studio pricing is disabled")

// ErrStripeDisabled is returned when the Stripe client is not configured.
var ErrStripeDisabled = errors.New("stripe is disabled")

// Service lists all functionality provided by the studio service.
type Service interface {
	// RecordProductionCharge persists a charge audit record. It is
	// idempotent per Stripe payment intent: recording the same intent twice
	// returns ErrChargeAlreadyRecorded and does not create a duplicate row.
	RecordProductionCharge(in CreateProductionChargeInput) (*ProductionCharge, error)
	// CreateStockRequestCharge charges a Studio stock request: it reads the
	// quote's config snapshot from Mongo, recomputes the exact price with the
	// active pricing rates, creates and confirms a Stripe PaymentIntent, and
	// persists the audit record with the server-computed values.
	CreateStockRequestCharge(ctx context.Context, in CreateStockRequestChargeInput) (*ProductionCharge, error)
	// ListProductionChargesByUser returns the audit records for a user.
	ListProductionChargesByUser(userID string) ([]ProductionCharge, error)
}

// ServiceImpl implements the studio Service.
type ServiceImpl struct {
	store  Store
	quotes QuoteReader
	engine PriceEngine
	charge ChargeClient
}

// NewService initiates a new studio service.
func NewService(store Store) Service {
	return &ServiceImpl{store: store}
}

// NewChargeService initiates a studio service wired for the full charge flow
// (quote reader + pricing engine + Stripe client). Any of the optional deps may
// be nil; the corresponding step reports its disabled error.
func NewChargeService(store Store, quotes QuoteReader, engine PriceEngine, charge ChargeClient) Service {
	return &ServiceImpl{store: store, quotes: quotes, engine: engine, charge: charge}
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

// CreateStockRequestCharge charges a Studio stock request end-to-end. The
// amount is never trusted from the client: it is recomputed from the quote's
// config snapshot and the active pricing rates at charge time.
func (s *ServiceImpl) CreateStockRequestCharge(ctx context.Context, in CreateStockRequestChargeInput) (*ProductionCharge, error) {
	if in.UserID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	if in.QuoteID == "" {
		return nil, fmt.Errorf("quote id is required")
	}
	if in.Quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	if in.StripeCustomerID == "" {
		return nil, fmt.Errorf("stripe customer id is required")
	}
	if in.IdempotencyKey == "" {
		return nil, fmt.Errorf("idempotency key is required")
	}

	// 1. Read the quote's config snapshot from Mongo.
	if s.quotes == nil {
		return nil, ErrPricingDisabled
	}
	quote, err := s.quotes.GetByID(ctx, in.QuoteID)
	if err != nil {
		if errors.Is(err, datastoremongo.ErrQuoteNotFound) {
			return nil, ErrQuoteNotFound
		}
		return nil, fmt.Errorf("read studio quote: %w", err)
	}

	// 1b. The quote must belong to the requesting user. A foreign quote is
	// treated as not found (404) so we don't leak whether a quote exists.
	if quote.UserID != "" && quote.UserID != in.UserID {
		return nil, ErrQuoteNotFound
	}

	// 2. Recompute the exact price with the active rates from the quote's
	// config snapshot and the requested quantity as the run size. The engine
	// returns the server-computed amount in cents plus the pricing hash and
	// engine version that produced it.
	if s.engine == nil {
		return nil, ErrPricingDisabled
	}
	amountCents, pricingHash, engineVersion, err := s.engine.Quote(ctx, quote.Config, in.Quantity)
	if err != nil {
		return nil, fmt.Errorf("recompute quote price: %w", err)
	}
	if amountCents <= 0 {
		return nil, fmt.Errorf("computed charge amount must be positive (got %d cents)", amountCents)
	}

	// 5. Create and confirm the Stripe PaymentIntent.
	if s.charge == nil {
		return nil, ErrStripeDisabled
	}
	intent, err := s.charge.CreateAndConfirm(ctx, StripeChargeInput{
		AmountCents:     amountCents,
		Currency:        "usd",
		CustomerID:      in.StripeCustomerID,
		PaymentMethodID: in.PaymentMethodID,
		IdempotencyKey:  in.IdempotencyKey,
		Metadata:        in.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("create stripe payment intent: %w", err)
	}

	// 6. Persist the audit record with the server-computed values.
	charge, err := s.RecordProductionCharge(CreateProductionChargeInput{
		UserID:              in.UserID,
		QuoteID:             in.QuoteID,
		AmountCents:         amountCents,
		Currency:            "usd",
		StripePaymentIntent: intent.ID,
		PricingHash:         pricingHash,
		EngineVersion:       engineVersion,
		Metadata:            in.Metadata,
	})
	if err != nil {
		return nil, err
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
