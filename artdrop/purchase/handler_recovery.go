package purchase

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/authguard"
	apierrors "github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func writeRecoveryError(w http.ResponseWriter, e *RecoveryError) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(e.Status)
	if len(e.body) > 0 {
		_, _ = w.Write(e.body)
		return
	}
	_ = json.NewEncoder(w).Encode(e)
}
func writeLookup(w http.ResponseWriter, result *PurchaseRecoveryResponse, err error) {
	if err != nil {
		var e *RecoveryError
		if !errors.As(err, &e) {
			e = recoveryError(503, "LOOKUP_UNAVAILABLE", "")
		}
		writeRecoveryError(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(result)
}
func reader(w http.ResponseWriter, r *http.Request) (string, bool, bool) {
	subject, any, err := authguard.PurchaseReader(r)
	if err != nil {
		status := http.StatusForbidden
		var request *apierrors.RequestError
		if errors.As(err, &request) {
			status = request.StatusCode
		}
		code := "FORBIDDEN"
		if status == 401 {
			code = "UNAUTHORIZED"
		}
		writeRecoveryError(w, recoveryError(status, code, ""))
		return "", false, false
	}
	return subject, any, true
}
func (h *Handler) GetPurchase() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, any, ok := reader(w, r)
		if !ok {
			return
		}
		id := mux.Vars(r)["purchaseId"]
		if _, err := uuid.Parse(id); err != nil {
			writeRecoveryError(w, recoveryError(400, "INVALID_PURCHASE_ID", ""))
			return
		}
		result, err := h.service.GetPurchaseRecovery(r.Context(), id)
		if err == nil && (result.Purchase == nil || (!any && result.Purchase.UserID != subject)) {
			writeRecoveryError(w, recoveryError(404, "PURCHASE_NOT_FOUND", ""))
			return
		}
		writeLookup(w, result, err)
	})
}
func (h *Handler) GetIntent() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, any, ok := reader(w, r)
		if !ok {
			return
		}
		intent := mux.Vars(r)["intentId"]
		if !validIntent(intent) {
			writeRecoveryError(w, recoveryError(400, "INVALID_INTENT_ID", ""))
			return
		}
		owner := r.URL.Query().Get("userId")
		if owner == "" {
			owner = subject
		}
		if owner != subject && !any {
			writeRecoveryError(w, recoveryError(403, "FORBIDDEN", intent))
			return
		}
		result, err := h.service.GetIntentRecovery(r.Context(), owner, intent)
		writeLookup(w, result, err)
	})
}
