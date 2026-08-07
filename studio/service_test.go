package studio

import (
	"errors"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestService(t *testing.T) Service {
	t.Helper()
	// Use a unique in-memory database per test so data does not leak between
	// test functions (the shared cache would otherwise persist rows across
	// tests and break idempotency assertions).
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&ProductionCharge{}); err != nil {
		t.Fatal(err)
	}
	return NewService(NewGormStore(db))
}

func validInput() CreateProductionChargeInput {
	return CreateProductionChargeInput{
		UserID:              "user-1",
		QuoteID:             "quote-1",
		AmountCents:         2500,
		Currency:            "usd",
		StripePaymentIntent: "pi_123",
		PricingHash:         "hash-abc",
		EngineVersion:       "v1",
	}
}

func TestRecordProductionChargeHappyPath(t *testing.T) {
	svc := newTestService(t)

	charge, err := svc.RecordProductionCharge(validInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if charge.ID == 0 {
		t.Fatal("expected charge to be persisted with an id")
	}
	if charge.AmountCents != 2500 {
		t.Fatalf("expected amount 2500, got %d", charge.AmountCents)
	}
	if charge.Currency != "usd" {
		t.Fatalf("expected currency usd, got %s", charge.Currency)
	}
	if charge.StripePaymentIntent != "pi_123" {
		t.Fatalf("expected payment intent pi_123, got %s", charge.StripePaymentIntent)
	}
	if charge.PricingHash != "hash-abc" {
		t.Fatalf("expected pricing hash hash-abc, got %s", charge.PricingHash)
	}
}

func TestRecordProductionChargeIdempotentByPaymentIntent(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.RecordProductionCharge(validInput()); err != nil {
		t.Fatalf("unexpected error on first record: %v", err)
	}

	// Same payment intent must not create a duplicate audit row.
	_, err := svc.RecordProductionCharge(validInput())
	if !errors.Is(err, ErrChargeAlreadyRecorded) {
		t.Fatalf("expected ErrChargeAlreadyRecorded, got %v", err)
	}

	charges, err := svc.ListProductionChargesByUser("user-1")
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if len(charges) != 1 {
		t.Fatalf("expected exactly 1 charge, got %d", len(charges))
	}
}

func TestRecordProductionChargeValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*CreateProductionChargeInput)
		wantErr bool
	}{
		{
			name:    "missing payment intent",
			mutate:  func(in *CreateProductionChargeInput) { in.StripePaymentIntent = "" },
			wantErr: true,
		},
		{
			name:    "zero amount",
			mutate:  func(in *CreateProductionChargeInput) { in.AmountCents = 0 },
			wantErr: true,
		},
		{
			name:    "negative amount",
			mutate:  func(in *CreateProductionChargeInput) { in.AmountCents = -100 },
			wantErr: true,
		},
		{
			name:    "missing user id",
			mutate:  func(in *CreateProductionChargeInput) { in.UserID = "" },
			wantErr: true,
		},
		{
			name:    "default currency when empty",
			mutate:  func(in *CreateProductionChargeInput) { in.Currency = "" },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t)
			in := validInput()
			tt.mutate(&in)

			charge, err := svc.RecordProductionCharge(in)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if charge.Currency != "usd" {
				t.Fatalf("expected default currency usd, got %s", charge.Currency)
			}
		})
	}
}

func TestListProductionChargesByUser(t *testing.T) {
	svc := newTestService(t)

	in1 := validInput()
	in1.StripePaymentIntent = "pi_1"
	in2 := validInput()
	in2.StripePaymentIntent = "pi_2"
	in2.QuoteID = "quote-2"

	if _, err := svc.RecordProductionCharge(in1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordProductionCharge(in2); err != nil {
		t.Fatal(err)
	}

	charges, err := svc.ListProductionChargesByUser("user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(charges) != 2 {
		t.Fatalf("expected 2 charges, got %d", len(charges))
	}

	// A different user sees nothing.
	other, err := svc.ListProductionChargesByUser("user-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("expected 0 charges for other user, got %d", len(other))
	}
}

func TestListProductionChargesByUserRequiresUserID(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.ListProductionChargesByUser(""); err == nil {
		t.Fatal("expected error for empty user id, got nil")
	}
}
