package purchase

import (
	"context"
	"net/http"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
)

type PurchaseRecoveryResponse struct {
	IntentID        *string            `json:"intentId"`
	ExecutionStatus string             `json:"executionStatus"`
	ResolutionCode  string             `json:"resolutionCode"`
	Purchase        *RecoveredPurchase `json:"purchase"`
	Financial       *PurchaseFinancial `json:"financial,omitempty"`
	UpdatedAt       *time.Time         `json:"updatedAt"`
}
type RecoveredPurchase struct {
	PurchaseID  string            `json:"purchaseId"`
	UserID      string            `json:"userId"`
	ArtworkKind string            `json:"artworkKind"`
	ArtworkID   string            `json:"artworkId"`
	Buyer       string            `json:"buyer"`
	Seller      string            `json:"seller"`
	EditionID   uint64            `json:"editionId"`
	RequestHash *string           `json:"requestHash"`
	HashVersion *int              `json:"hashVersion"`
	Metadata    string            `json:"metadata"`
	Amounts     PurchaseAmounts   `json:"amounts"`
	Financial   PurchaseFinancial `json:"financial"`
	Escrow      PurchaseEscrow    `json:"escrow"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}
type PurchaseAmounts struct {
	ArtworkCents      int64   `json:"artworkCents"`
	PresentationCents *int64  `json:"presentationCents"`
	ShippingCents     int64   `json:"shippingCents"`
	TotalCents        int64   `json:"totalCents"`
	Currency          string  `json:"currency"`
	PlatformFeeCents  int64   `json:"platformFeeCents"`
	FlowAmount        float64 `json:"flowAmount"`
	FlowPriceUSD      float64 `json:"flowPriceUsd"`
}
type PurchaseFinancial struct {
	Status                string     `json:"status"`
	StripePaymentIntentID *string    `json:"stripePaymentIntentId"`
	StripeCustomerID      *string    `json:"stripeCustomerId"`
	CustomerSource        *string    `json:"customerSource"`
	StripeStatusObserved  *string    `json:"stripeStatusObserved"`
	StripeObservedAt      *time.Time `json:"stripeObservedAt"`
	AmountReceivedCents   *int64     `json:"amountReceivedCents"`
	StripeAccountID       *string    `json:"stripeAccountId"`
	Livemode              *bool      `json:"livemode"`
	VerifiedAt            *time.Time `json:"verifiedAt"`
	EvidenceSource        string     `json:"evidenceSource"`
}
type PurchaseEscrow struct {
	Status        string  `json:"status"`
	LegacyStatus  string  `json:"legacyStatus"`
	ChipID        *string `json:"chipId"`
	JobID         *string `json:"jobId"`
	EscrowID      *uint64 `json:"escrowId"`
	CertificateID *uint64 `json:"certificateId"`
	Nonce         uint64  `json:"nonce"`
	UnlockAt      float64 `json:"unlockAt"`
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func financial(pi, customer, status *string, observed *time.Time, legacy bool) PurchaseFinancial {
	f := PurchaseFinancial{Status: "unknown", StripePaymentIntentID: pi, StripeCustomerID: customer, StripeStatusObserved: status, StripeObservedAt: observed, EvidenceSource: "wallet_reservation"}
	if customer != nil {
		f.CustomerSource = handlers.Ptr("request")
	}
	if observed != nil {
		f.EvidenceSource = "stripe_create_response_unverified"
	}
	if legacy {
		f.EvidenceSource = "legacy_record"
	}
	return f
}
func recoveredPurchase(p *PurchaseCharge) *RecoveredPurchase {
	escrowStatus := "unknown"
	switch p.Status {
	case PurchaseStatusPaidPendingEscrow:
		escrowStatus = "pending"
	case PurchaseStatusEscrowOpening:
		escrowStatus = "opening"
	}
	return &RecoveredPurchase{
		PurchaseID: p.PurchaseID, UserID: p.UserID, ArtworkKind: p.ArtworkKind, ArtworkID: p.ArtworkID, Buyer: p.Buyer, Seller: p.Seller,
		EditionID: p.EditionID, RequestHash: p.RequestHash, HashVersion: p.HashVersion, Metadata: p.Metadata,
		Amounts:   PurchaseAmounts{ArtworkCents: p.AmountCents, ShippingCents: p.ShippingCents, TotalCents: p.AmountCents + p.ShippingCents, Currency: p.Currency, PlatformFeeCents: p.PlatformFeeCents, FlowAmount: p.FlowAmount, FlowPriceUSD: p.FlowPriceUSD},
		Financial: financial(nullableString(p.StripePaymentIntent), p.StripeCustomerID, p.StripeStatusObserved, p.StripeObservedAt, p.ChargeIntentID == nil),
		Escrow:    PurchaseEscrow{Status: escrowStatus, LegacyStatus: p.Status, ChipID: nullableString(p.ChipID), JobID: nullableString(p.EscrowJobID), EscrowID: p.EscrowID, CertificateID: p.CertificateID, Nonce: p.Nonce, UnlockAt: p.UnlockAt},
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}
func recoveryResponse(row *handlers.PurchaseIntent, p *PurchaseCharge) *PurchaseRecoveryResponse {
	r := &PurchaseRecoveryResponse{ExecutionStatus: "unknown", ResolutionCode: "RECONCILIATION_REQUIRED"}
	if row != nil {
		r.IntentID, r.UpdatedAt = row.IntentID, row.UpdatedAt
		f := financial(row.StripePaymentIntentID, row.StripeCustomerID, row.StripeStatusObserved, row.StripeObservedAt, false)
		r.Financial = &f
		switch handlers.Value(row.ExecutionState) {
		case "reserved", "executing":
			if row.LeaseUntil != nil && time.Now().Before(*row.LeaseUntil) {
				r.ExecutionStatus = "in_progress"
				r.ResolutionCode = "INTENT_IN_PROGRESS"
			}
		case "not_charged":
			if !handlers.Value(row.EffectMayHaveStarted) {
				r.ExecutionStatus = "not_charged"
				r.ResolutionCode = "NOT_CHARGED"
				f.Status = "not_charged"
				f.EvidenceSource = "wallet_pre_effect_validation"
			}
		}
	}
	if p != nil {
		r.IntentID, r.UpdatedAt = p.ChargeIntentID, &p.UpdatedAt
		r.Purchase, r.Financial = recoveredPurchase(p), nil
		r.ExecutionStatus, r.ResolutionCode = "recorded", "FINANCIAL_VERIFICATION_REQUIRED"
		if row != nil && (handlers.Value(row.RequestHash) != handlers.Value(p.RequestHash) || handlers.Value(row.HashVersion) != handlers.Value(p.HashVersion) ||
			handlers.Value(row.OwnerUserID) != p.UserID || handlers.Value(row.IntentID) != handlers.Value(p.ChargeIntentID) ||
			handlers.Value(p.ChargeOperation) != handlers.PurchaseOperation ||
			(handlers.Value(row.StripePaymentIntentID) != "" && handlers.Value(row.StripePaymentIntentID) != p.StripePaymentIntent) ||
			(handlers.Value(row.PurchaseID) != "" && handlers.Value(row.PurchaseID) != p.PurchaseID) ||
			handlers.Value(row.ExecutionState) == "not_charged") {
			r.ExecutionStatus, r.ResolutionCode = "unknown", "EVIDENCE_CONFLICT"
			f := financial(row.StripePaymentIntentID, row.StripeCustomerID, row.StripeStatusObserved, row.StripeObservedAt, false)
			r.Financial = &f
		}
	}
	return r
}
func (s *ServiceImpl) GetPurchaseRecovery(ctx context.Context, id string) (*PurchaseRecoveryResponse, error) {
	if s.store == nil {
		return nil, recoveryError(503, "LOOKUP_UNAVAILABLE", "")
	}
	p, err := s.store.GetPurchaseCharge(ctx, id)
	if err != nil {
		return nil, recoveryError(503, "LOOKUP_UNAVAILABLE", "")
	}
	if p == nil {
		return nil, recoveryError(404, "PURCHASE_NOT_FOUND", "")
	}
	if p.ChargeIntentID != nil {
		durable, ok := s.store.(DurableStore)
		if !ok {
			return nil, recoveryError(503, "LOOKUP_UNAVAILABLE", *p.ChargeIntentID)
		}
		row, indexed, err := durable.ReadRecovery(ctx, p.UserID, *p.ChargeIntentID)
		if err != nil {
			return nil, recoveryError(503, "LOOKUP_UNAVAILABLE", *p.ChargeIntentID)
		}
		result := recoveryResponse(row, p)
		if indexed == nil || indexed.PurchaseID != p.PurchaseID {
			result.ExecutionStatus, result.ResolutionCode = "unknown", "EVIDENCE_CONFLICT"
		}
		return result, nil
	}
	return recoveryResponse(nil, p), nil
}
func (s *ServiceImpl) GetIntentRecovery(ctx context.Context, owner, intent string) (*PurchaseRecoveryResponse, error) {
	durable, ok := s.store.(DurableStore)
	if !ok {
		return nil, recoveryError(503, "LOOKUP_UNAVAILABLE", intent)
	}
	row, p, err := durable.ReadRecovery(ctx, owner, intent)
	if err != nil {
		return nil, recoveryError(503, "LOOKUP_UNAVAILABLE", intent)
	}
	if row == nil && p == nil {
		return nil, recoveryError(http.StatusNotFound, "INTENT_NOT_FOUND", intent)
	}
	return recoveryResponse(row, p), nil
}
