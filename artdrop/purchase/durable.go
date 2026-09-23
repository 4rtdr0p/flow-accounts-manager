package purchase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
)

var intentPattern = regexp.MustCompile(`^purchase-v2:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validIntent(id string) bool { return len(id) <= 128 && intentPattern.MatchString(id) }

// RecoveryError has deliberately conservative public messages. HTTP status is
// never evidence that the payment provider rejected a charge.
type RecoveryError struct {
	Status     int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	IntentID   string `json:"intentId,omitempty"`
	PurchaseID string `json:"purchaseId,omitempty"`
	cause      error
	body       []byte
}

func (e *RecoveryError) Error() string { return e.Code + ": " + e.Message }
func (e *RecoveryError) Unwrap() error { return e.cause }
func recoveryError(status int, code, intent string) *RecoveryError {
	message := "Consult the purchase recovery endpoint; do not retry with another identity."
	switch code {
	case "UNAUTHORIZED":
		message = "A valid bearer token is required."
	case "FORBIDDEN":
		message = "The token does not authorize this purchase operation."
	case "INVALID_INTENT_ID":
		message = "intentId must be purchase-v2:<UUID>."
	case "INVALID_PURCHASE_ID":
		message = "purchaseId must be a UUID."
	case "INVALID_REQUEST":
		message = "Invalid request body or unknown fields."
	case "IDEMPOTENCY_TERMS_CONFLICT":
		message = "This financial identity is bound to different request terms."
	case "LEGACY_INTENT_REQUIRES_REVIEW":
		message = "Legacy financial identities require review; do not replace the identity to retry a charge."
	case "PURCHASE_NOT_FOUND", "INTENT_NOT_FOUND":
		message = "Record not found. Absence does not prove that the provider did not charge."
	case "RESULT_BODY_EXPIRED":
		message = "The original response is unavailable; use the recovery GET with the same identity."
	}
	return &RecoveryError{Status: status, Code: code, Message: message, IntentID: intent}
}

// Hash a versioned, typed request; never round uint64 through float64. The
// identity and actor are not commercial terms. unlockAt is intentionally absent.
// chargeTermsV1 is frozen: changing canonical fields requires a new hash version.
type chargeTermsV1 struct {
	Version          int         `json:"version"`
	UserID           string      `json:"userId"`
	ArtworkKind      ArtworkKind `json:"artworkKind"`
	ArtworkID        string      `json:"artworkId"`
	StripeCustomerID string      `json:"stripeCustomerId"`
	PaymentMethodID  string      `json:"paymentMethodId"`
	Metadata         string      `json:"metadata"`
	ShippingCents    int64       `json:"shippingCents"`
	Buyer            string      `json:"buyer"`
	Seller           string      `json:"seller"`
	EditionID        uint64      `json:"editionId"`
	ChipID           string      `json:"chipId"`
	Nonce            uint64      `json:"nonce"`
	CertificateID    uint64      `json:"certificateId"`
}

func requestHash(in CreatePurchaseChargeInput) string {
	b, _ := json.Marshal(chargeTermsV1{
		Version: 1, UserID: in.UserID, ArtworkKind: in.ArtworkKind, ArtworkID: in.ArtworkID, StripeCustomerID: in.StripeCustomerID,
		PaymentMethodID: in.PaymentMethodID, Metadata: in.Metadata, ShippingCents: in.ShippingCents,
		Buyer: in.Buyer, Seller: in.Seller, EditionID: in.EditionID, ChipID: in.ChipID, Nonce: in.Nonce, CertificateID: in.CertificateID,
	})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type DurabilityOptions struct{ ResponseRetention, Lease time.Duration }

func DefaultDurabilityOptions() DurabilityOptions {
	return DurabilityOptions{30 * 24 * time.Hour, 120 * time.Second}
}
func (o DurabilityOptions) Validate() error {
	if o.ResponseRetention <= 0 || o.Lease < 120*time.Second {
		return fmt.Errorf("purchase retention must be positive and lease at least 120s")
	}
	return nil
}

// DurableStore is mandatory for charge; implementations of the older Store
// interface cannot accidentally enable a path to Stripe without a reservation.
type DurableStore interface {
	Reserve(context.Context, CreatePurchaseChargeInput, string) (*handlers.PurchaseIntent, bool, error)
	MarkEffect(context.Context, *handlers.PurchaseIntent) error
	ObservePayment(context.Context, *handlers.PurchaseIntent, string, string, time.Time) error
	FailIntent(context.Context, *handlers.PurchaseIntent, bool, *RecoveryError) error
	CompleteCharge(context.Context, *handlers.PurchaseIntent, *PurchaseCharge) error
	PurchaseByPI(context.Context, string) (*PurchaseCharge, error)
	ReadRecovery(context.Context, string, string) (*handlers.PurchaseIntent, *PurchaseCharge, error)
}

func replayIntent(row *handlers.PurchaseIntent, hash string) (*PurchaseCharge, error) {
	id := handlers.Value(row.IntentID)
	if handlers.Value(row.RequestHash) != hash || handlers.Value(row.HashVersion) != 1 {
		return nil, recoveryError(409, "IDEMPOTENCY_TERMS_CONFLICT", id)
	}
	state := handlers.Value(row.ExecutionState)
	if state == "recorded" || state == "not_charged" {
		if len(row.Body) == 0 || row.ResponseExpiresAt == nil || !time.Now().Before(*row.ResponseExpiresAt) {
			e := recoveryError(409, "RESULT_BODY_EXPIRED", id)
			e.PurchaseID = handlers.Value(row.PurchaseID)
			return nil, e
		}
		if state == "not_charged" {
			e := &RecoveryError{Status: row.StatusCode, body: row.Body}
			if err := json.Unmarshal(row.Body, e); err != nil {
				return nil, recoveryError(503, "LOOKUP_UNAVAILABLE", id)
			}
			return nil, e
		}
		var charge PurchaseCharge
		if err := json.Unmarshal(row.Body, &charge); err != nil {
			return nil, recoveryError(503, "LOOKUP_UNAVAILABLE", id)
		}
		charge.responseBody = row.Body
		return &charge, nil
	}
	if (state == "reserved" || state == "executing") && row.LeaseUntil != nil && time.Now().Before(*row.LeaseUntil) {
		return nil, recoveryError(http.StatusConflict, "INTENT_IN_PROGRESS", id)
	}
	return nil, recoveryError(http.StatusConflict, "RECONCILIATION_REQUIRED", id)
}
