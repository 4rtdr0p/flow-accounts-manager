package exchange

import "sort"

// Role is a Payload identity role carried in the assertion. The wallet — not
// Payload — decides which wallet scopes each role maps to (see DefaultRoleScopes);
// the assertion never carries scopes.
type Role string

const (
	RoleUser       Role = "user"
	RoleOperations Role = "operations"
	RoleAdmin      Role = "admin"
)

// Scope strings below are the exact x-required-scopes values from openapi.yml.
// Keeping them as named constants (rather than scattered literals) makes the
// role map auditable and keeps a typo from silently granting the wrong access.
const (
	// Read scopes.
	scopeAccountRead      = "account.read"
	scopeJobRead          = "job.read"
	scopeOpsRead          = "ops.read"
	scopePricingRead      = "pricing.read"
	scopeStudioChargeRead = "studio.charge.read"
	scopeSystemRead       = "system.read"
	scopeTokenRead        = "token.read"
	scopeTransactionRead  = "transaction.read"
	scopeHealthRead       = "health.read"

	// Write / action scopes.
	scopeAccountCreate      = "account.create"
	scopeAccountSetup       = "account.setup"
	scopeStudioChargeCreate = "studio.charge.create"
	scopeAccountSign        = "account.sign"
	scopeEscrowVoid         = "account.artdrop.escrow.void"
	scopeEscrowReescrow     = "account.artdrop.escrow.reescrow"
	scopeEscrowActivate     = "account.artdrop.escrow.activate"
	scopeOriginalCreate     = "account.artdrop.original.create"
	scopeEditionCreate      = "account.artdrop.edition.create"
	scopeSystemWrite        = "system.write"
)

// readScopes are granted to every role. These are all read-only endpoints.
// health.read is included for completeness even though the health routes are
// auth-exempt.
var readScopes = []string{
	scopeAccountRead,
	scopeJobRead,
	scopeOpsRead,
	scopePricingRead,
	scopeStudioChargeRead,
	scopeSystemRead,
	scopeTokenRead,
	scopeTransactionRead,
	scopeHealthRead,
}

// userScopes is the base set every authenticated end-user gets.
//
// account.sign is a TRANSITIONAL god-scope: it authorizes the raw
// /accounts/{address}/sign endpoint, which lets the front sign arbitrary
// transactions. It is granted only until the front migrates onto the
// higher-level typed endpoints (transfer, escrow, purchases:charge, ...). To
// remove it later, delete the single scopeAccountSign entry from this slice —
// nothing else depends on it being here.
var userScopes = concatScopes(readScopes, []string{
	scopeAccountCreate,
	scopeAccountSetup,
	scopeStudioChargeCreate, // also covers /purchases:charge (issue #107)
	scopeAccountSign,        // TRANSITIONAL — see note above
})

// operationsScopes is userScopes plus the operator-only actions: escrow
// lifecycle, original/edition creation, and system.* writes.
var operationsScopes = concatScopes(userScopes, []string{
	scopeEscrowVoid,
	scopeEscrowReescrow,
	scopeEscrowActivate,
	scopeOriginalCreate,
	scopeEditionCreate,
	scopeSystemWrite,
})

// DefaultRoleScopes is the wallet's role→scope policy. admin is a superset role
// and currently mirrors operations exactly.
var DefaultRoleScopes = map[Role][]string{
	RoleUser:       userScopes,
	RoleOperations: operationsScopes,
	RoleAdmin:      operationsScopes,
}

// ScopesForRole returns the scopes granted to role, and whether the role is
// known. The returned slice is a defensive copy the caller may mutate.
func ScopesForRole(role string) ([]string, bool) {
	scopes, ok := DefaultRoleScopes[Role(role)]
	if !ok {
		return nil, false
	}
	out := make([]string, len(scopes))
	copy(out, scopes)
	return out, true
}

// concatScopes returns the de-duplicated, sorted union of the given scope
// slices. Sorting keeps the minted token's scope claim stable regardless of
// input order.
func concatScopes(groups ...[]string) []string {
	seen := map[string]struct{}{}
	for _, g := range groups {
		for _, s := range g {
			seen[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
