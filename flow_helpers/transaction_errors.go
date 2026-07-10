package flow_helpers

import "strings"

const vaultExistsErrSnippet = "vault exists"

// IsVaultExistsError reports whether Flow surfaced the expected setup idempotency
// panic for an already-initialized vault.
func IsVaultExistsError(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(strings.ToLower(err.Error()), vaultExistsErrSnippet)
}
