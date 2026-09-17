package exchange

import "sort"

// Role is a Payload identity role carried in the assertion. The wallet — not
// Payload — decides which wallet scopes each role maps to (see DefaultRoleScopes);
// the assertion never carries scopes.
type Role string

const (
	RoleUser       Role = "user"
	RoleArtist     Role = "artist"
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

	// User action scopes.
	scopeScriptExecute      = "script.execute"
	scopeAccountCreate      = "account.create"
	scopeAccountSetup       = "account.setup"
	scopeStudioChargeCreate = "studio.charge.create"
	scopeAccountTransfer    = "account.transfer"
	scopeEscrowActivate     = "account.artdrop.escrow.activate"

	// Operations action scopes.
	scopeEscrowVoid      = "account.artdrop.escrow.void"
	scopeEscrowReescrow  = "account.artdrop.escrow.reescrow"
	scopeOriginalCreate  = "account.artdrop.original.create"
	scopeEditionCreate   = "account.artdrop.edition.create"
	scopeArtistOnboard   = "account.artdrop.artist.onboard"
	scopeAccountGraduate = "account.key.graduate"
	scopeAccountKeySync  = "account.key.sync"
	scopeWatchlistWrite  = "watchlist.write"
	scopeOpsRun          = "ops.run"
	scopeSystemWrite     = "system.write"
	scopeChipProvision   = "chip.provision"
	scopeEscrowOpenPaid  = "account.artdrop.escrow.open-paid"
	// scopeStudioChargeOnBehalf lets an operator create a studio charge on behalf
	// of another user (not for their own custodial account). It is a capability
	// checked by a downstream guard, not a requirement of any route, so it is not
	// listed in openapi.yml. Operations+ only; never granted to end users.
	scopeStudioChargeOnBehalf = "studio.charge.create.onbehalf"
	// scopeArtistOnBehalf lets an operator create an Original/Edition (or onboard
	// an artist) on behalf of an artist other than the token's own flow_address.
	// Like scopeStudioChargeOnBehalf it is a capability checked by a downstream
	// guard (requireArtistSubject), not a requirement of any route, so it is NOT
	// listed in openapi.yml. Operations+ only; never granted to the artist role
	// so that self-service artist tokens stay bound to their own address.
	scopeArtistOnBehalf = "account.artdrop.artist.onbehalf"

	// Admin-only break-glass scopes (see adminScopes).
	scopeAccountSign       = "account.sign"
	scopeTransactionCreate = "transaction.create"
	scopeTokenWrite        = "token.write"
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

// userScopes is the base set every authenticated end-user gets: all reads plus
// the typed end-user actions. No god-scope here — the front uses the typed
// endpoints (transfer, purchases:charge, escrow activate). escrow.activate is a
// user action because the activation transaction is signed by the buyer
// (activator == buyer on-chain).
var userScopes = concatScopes(readScopes, []string{
	scopeScriptExecute,
	scopeAccountCreate,
	scopeAccountSetup,
	scopeStudioChargeCreate, // also covers /purchases:charge (issue #107)
	scopeAccountTransfer,
	scopeEscrowActivate,
})

// artistScopes is the self-service artist role: every end-user scope plus the
// ability to create their OWN Originals/Editions and onboard themselves. It
// deliberately omits scopeArtistOnBehalf so that requireArtistSubject keeps an
// artist token bound to its own flow_address — an artist can act only for
// themselves, never for another artist. original.create/edition.create/
// artist.onboard stay in operationsScopes too (an operator needs them to reach
// the same routes); they are granted here in addition, not moved.
var artistScopes = concatScopes(userScopes, []string{
	scopeOriginalCreate,
	scopeEditionCreate,
	scopeArtistOnboard,
})

// operationsScopes is userScopes plus the operator-only actions: the rest of
// the escrow lifecycle, original/edition creation, artist onboarding, custodial
// key management, chip provisioning, watchlist writes, and ops/system.* writes.
var operationsScopes = concatScopes(userScopes, []string{
	scopeEscrowVoid,
	scopeEscrowReescrow,
	scopeOriginalCreate,
	scopeEditionCreate,
	scopeArtistOnboard,
	scopeAccountGraduate,
	scopeAccountKeySync,
	scopeWatchlistWrite,
	scopeOpsRun,
	scopeSystemWrite,
	scopeChipProvision,
	scopeEscrowOpenPaid,
	scopeStudioChargeOnBehalf,
	scopeArtistOnBehalf,
})

// adminScopes is operationsScopes plus the break-glass scopes that must never
// be handed to an end-user or operator: the raw signing endpoint (account.sign)
// and the raw transaction/token-config endpoints. admin is a STRICT superset of
// operations — it is not merely an alias.
var adminScopes = concatScopes(operationsScopes, []string{
	scopeAccountSign,       // break-glass: raw /accounts/{address}/sign
	scopeTransactionCreate, // break-glass: raw transaction submission
	scopeTokenWrite,        // break-glass: token template config
})

// DefaultRoleScopes is the wallet's role→scope policy.
var DefaultRoleScopes = map[Role][]string{
	RoleUser:       userScopes,
	RoleArtist:     artistScopes,
	RoleOperations: operationsScopes,
	RoleAdmin:      adminScopes,
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
