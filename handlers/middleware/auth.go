package middleware

import (
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
	PathPattern   *regexp.Regexp
	RequiredScope string
}

type AuthClaims struct {
	Scope string `json:"scope"`
	jwt.RegisteredClaims
}

func AuthHandler(h http.Handler, opts AuthOptions) http.Handler {
	if !opts.Enabled {
		return h
	}

	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
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

		h.ServeHTTP(rw, r)
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
