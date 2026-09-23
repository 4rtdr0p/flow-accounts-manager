package purchase

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"gorm.io/gorm"
)

func (s *GormStore) Reserve(ctx context.Context, in CreatePurchaseChargeInput, hash string) (*handlers.PurchaseIntent, bool, error) {
	if s.db == nil {
		return nil, false, ErrChargeRecordFailed
	}
	if err := s.durability.Validate(); err != nil {
		return nil, false, err
	}
	row, won, err := handlers.ReservePurchaseIntent(ctx, s.db, in.UserID, in.ActorSubject, in.IdempotencyKey, hash, in.StripeCustomerID, s.durability.Lease)
	if err == nil && !won && row.ResponseExpiresAt != nil && !time.Now().Before(*row.ResponseExpiresAt) && len(row.Body) != 0 {
		// Lazy body retention also works when the generic middleware is disabled.
		err = s.db.WithContext(ctx).Model(&handlers.PurchaseIntent{}).Where("key = ? AND record_kind = 'purchase'", row.Key).UpdateColumn("body", nil).Error
		row.Body = nil
	}
	return row, won, err
}
func (s *GormStore) MarkEffect(ctx context.Context, row *handlers.PurchaseIntent) error {
	return handlers.CASPurchaseIntent(ctx, s.db, row, []string{"reserved"}, map[string]interface{}{"execution_state": "executing", "effect_may_have_started": true})
}
func (s *GormStore) ObservePayment(ctx context.Context, row *handlers.PurchaseIntent, id, status string, observed time.Time) error {
	return handlers.CASPurchaseIntent(ctx, s.db, row, []string{"executing"}, map[string]interface{}{"stripe_payment_intent_id": id, "stripe_status_observed": status, "stripe_observed_at": observed})
}
func (s *GormStore) FailIntent(ctx context.Context, row *handlers.PurchaseIntent, effect bool, e *RecoveryError) error {
	changes := map[string]interface{}{"execution_state": "unknown", "error_code": e.Code}
	from := []string{"reserved", "executing"}
	var body []byte
	if !effect {
		var err error
		body, err = json.Marshal(e)
		if err != nil {
			return err
		}
		changes["execution_state"], changes["completed"] = "not_charged", true
		changes["body"], changes["status_code"], changes["content_type"] = body, e.Status, "application/json"
		changes["response_expires_at"] = time.Now().UTC().Add(s.durability.ResponseRetention)
		from = []string{"reserved"}
	}
	err := handlers.CASPurchaseIntent(ctx, s.db, row, from, changes)
	if err == nil && len(body) != 0 {
		e.body = body
	}
	return err
}
func (s *GormStore) CompleteCharge(ctx context.Context, row *handlers.PurchaseIntent, charge *PurchaseCharge) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if charge.ID == 0 {
			if err := tx.Create(charge).Error; err != nil {
				return err
			}
		}
		body, err := json.Marshal(charge)
		if err != nil {
			return err
		}
		if err := handlers.CASPurchaseIntent(ctx, tx, row, []string{"executing"}, map[string]interface{}{
			"execution_state": "recorded", "purchase_id": charge.PurchaseID, "completed": true,
			"body": body, "status_code": 201, "content_type": "application/json", "response_expires_at": time.Now().UTC().Add(s.durability.ResponseRetention),
		}); err != nil {
			return err
		}
		charge.responseBody = body
		return nil
	})
}
func (s *GormStore) PurchaseByPI(ctx context.Context, pi string) (*PurchaseCharge, error) {
	var p PurchaseCharge
	err := s.db.WithContext(ctx).Where("stripe_payment_intent_id = ?", pi).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &p, err
}

// Both reads use the same SQL snapshot, avoiding a mixed pre/post-commit view.
func (s *GormStore) ReadRecovery(ctx context.Context, owner, intent string) (row *handlers.PurchaseIntent, p *PurchaseCharge, err error) {
	if s.db == nil {
		return nil, nil, ErrChargeRecordFailed
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var readErr error
		row, readErr = handlers.ReadPurchaseIntent(ctx, tx, owner, intent)
		if readErr != nil {
			return readErr
		}
		var charge PurchaseCharge
		readErr = tx.Where("user_id = ? AND charge_operation = ? AND charge_intent_id = ?", owner, handlers.PurchaseOperation, intent).First(&charge).Error
		if errors.Is(readErr, gorm.ErrRecordNotFound) {
			return nil
		}
		// Do not trust case-insensitive database collations as an ownership
		// check. A tuple must match the token/request strings byte for byte.
		if readErr == nil && charge.UserID == owner && handlers.Value(charge.ChargeOperation) == handlers.PurchaseOperation && handlers.Value(charge.ChargeIntentID) == intent {
			p = &charge
		}
		return readErr
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return
}
