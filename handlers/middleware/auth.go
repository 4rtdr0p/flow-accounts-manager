package middleware

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	jwt "github.com/golang-jwt/jwt/v5"
	log "github.com/sirupsen/logrus"
)

type AuthOptions struct {
	Enabled  bool
	Secret   string
	Issuer   string
	Audience string
	Rules    []AuthRule
}

type AuthRule struct {
	Method        string
	PathTemplate  string
	PathPattern   *regexp.Regexp
	RequiredScope string
}

type AuthClaims struct {
	Scope string `json:"scope"`
	// FlowAddress is the caller's custodial Flow address, carried through from
	// the Payload identity assertion at token-exchange time. It binds the token
	// to a specific on-chain identity so downstream guards can enforce that the
	// caller is acting on their own account. Empty when the assertion did not
	// carry it (backward-compatible: the front does not send it yet).
	FlowAddress string `json:"flow_address,omitempty"`
	jwt.RegisteredClaims
}

type claimsContextKey struct{}

// ClaimsFromContext returns the validated claims of the request's bearer
// token, if auth ran and succeeded for this request.
func ClaimsFromContext(ctx context.Context) (*AuthClaims, bool) {
	claims, ok := ctx.Value(claimsContextKey{}).(*AuthClaims)
	return claims, ok
}

// ContextWithClaims attaches claims to ctx the same way AuthHandler does.
// Exported for tests of downstream handlers that read claims via
// ClaimsFromContext.
func ContextWithClaims(ctx context.Context, claims *AuthClaims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

// authExemptPaths lists the routes that must be reachable without a wallet
// bearer token even when auth is enabled:
//   - the health endpoints, because Kubernetes readiness/liveness probes never
//     send a token and a 401 here means the pod never becomes ready and the
//     rollout stalls forever;
//   - the token-exchange endpoint, whose authentication IS the Payload identity
//     assertion in its body — requiring a wallet token to obtain a wallet token
//     would be circular.
//
// Deliberately a compile-time allowlist matched on exact method+path, not a
// prefix or an env-configurable list: a prefix on /v1/health would silently
// exempt any future /v1/health/* route, and an env-configurable list is a
// silent security hole waiting for a typo.
var authExemptPaths = map[string]struct{}{
	http.MethodGet + " /v1/health/ready":    {},
	http.MethodGet + " /v1/health/liveness": {},
	http.MethodPost + " /v1/auth/token":     {},
}

func isAuthExempt(method, path string) bool {
	_, ok := authExemptPaths[method+" "+path]
	return ok
}

var pathParamPattern = regexp.MustCompile(`\\\{[^}]+\\\}`)

func NewAuthRule(method string, pathTemplate string, requiredScope string) AuthRule {
	return AuthRule{
		Method:        method,
		PathTemplate:  pathTemplate,
		PathPattern:   compilePathTemplate(pathTemplate),
		RequiredScope: requiredScope,
	}
}

func (r AuthRule) Key() string {
	return r.Method + " " + r.PathTemplate
}

func AuthHandler(h http.Handler, opts AuthOptions) http.Handler {
	if !opts.Enabled {
		return h
	}

	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if isAuthExempt(r.Method, r.URL.Path) {
			h.ServeHTTP(rw, r)
			return
		}

		requiredScope, ok := requiredScopeForRequest(opts.Rules, r.Method, r.URL.Path)
		if !ok {
			log.WithFields(log.Fields{"method": r.Method, "path": r.URL.Path}).Warn("auth denied: endpoint scope missing")
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			log.WithFields(log.Fields{"method": r.Method, "path": r.URL.Path, "reason": "missing_or_invalid_bearer"}).Warn("auth failed")
			http.Error(rw, "missing or invalid bearer token", http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		claims := AuthClaims{}

		parser := jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithExpirationRequired(),
		)

		token, err := parser.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (any, error) {
			if opts.Secret == "" {
				return nil, errors.New("auth secret is empty")
			}
			return []byte(opts.Secret), nil
		})
		if err != nil || !token.Valid {
			log.WithFields(log.Fields{"method": r.Method, "path": r.URL.Path, "reason": "invalid_or_expired_token", "error": err}).Warn("auth failed")
			http.Error(rw, "invalid or expired token", http.StatusUnauthorized)
			return
		}

		if opts.Issuer != "" && claims.Issuer != opts.Issuer {
			log.WithFields(log.Fields{"method": r.Method, "path": r.URL.Path, "reason": "issuer_mismatch"}).Warn("auth failed")
			http.Error(rw, "invalid issuer", http.StatusUnauthorized)
			return
		}

		if opts.Audience != "" {
			matched := false
			for _, aud := range claims.Audience {
				if aud == opts.Audience {
					matched = true
					break
				}
			}
			if !matched {
				log.WithFields(log.Fields{"method": r.Method, "path": r.URL.Path, "reason": "audience_mismatch"}).Warn("auth failed")
				http.Error(rw, "invalid audience", http.StatusUnauthorized)
				return
			}
		}

		scopeClaim := claims.Scope
		if scopeClaim == "" {
			log.WithFields(log.Fields{"method": r.Method, "path": r.URL.Path, "reason": "scope_claim_missing"}).Warn("auth failed")
			http.Error(rw, "invalid token scope", http.StatusUnauthorized)
			return
		}

		if !hasScope(scopeClaim, requiredScope) {
			log.WithFields(log.Fields{"method": r.Method, "path": r.URL.Path, "required_scope": requiredScope, "reason": "scope_denied"}).Warn("auth denied")
			http.Error(rw, "insufficient scope", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), claimsContextKey{}, &claims)
		h.ServeHTTP(rw, r.WithContext(ctx))
	})
}

func hasScope(scopeClaim string, required string) bool {
	for _, s := range strings.Fields(scopeClaim) {
		if s == "*" || s == required {
			return true
		}
	}
	return false
}

func requiredScopeForRequest(rules []AuthRule, method string, path string) (string, bool) {
	for _, rule := range rules {
		if rule.Method != method {
			continue
		}
		if rule.PathPattern != nil && rule.PathPattern.MatchString(path) {
			return rule.RequiredScope, true
		}
	}
	return "", false
}

func compilePathTemplate(pathTemplate string) *regexp.Regexp {
	expr := regexp.QuoteMeta(pathTemplate)
	expr = pathParamPattern.ReplaceAllString(expr, `[^/]+`)
	return regexp.MustCompile("^" + expr + "$")
}
