package flow_helpers

import (
	"errors"
	"testing"
)

func TestIsVaultExistsError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		if IsVaultExistsError(nil) {
			t.Fatal("expected false for nil error")
		}
	})

	t.Run("matches cadence panic message", func(t *testing.T) {
		err := errors.New("transaction execution failed: [Error Code: 1101] cadence runtime error: panic: vault exists")
		if !IsVaultExistsError(err) {
			t.Fatal("expected vault exists error to match")
		}
	})

	t.Run("rejects unrelated errors", func(t *testing.T) {
		err := errors.New("transaction execution failed: missing capability")
		if IsVaultExistsError(err) {
			t.Fatal("expected unrelated error not to match")
		}
	})
}
