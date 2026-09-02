// Package exchange implements the token-exchange flow that turns a short-lived
// identity assertion signed by Payload (RS256) into a short-lived wallet access
// token (HS256) carrying the scopes the wallet's own policy grants the asserted
// role. The wallet is the authorization authority: Payload proves identity
// (sub + role), the wallet decides scopes.
package exchange

import (
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/handlers/middleware"
	jwt "github.com/golang-jwt/jwt/v5"
)

// Sentinel errors. The HTTP handler maps ErrNotConfigured to 503 and every
// other error here to 401.
var (
	// ErrNotConfigured means the exchange endpoint is not usable because the
	// Payload public key and/or the wallet signing secret are unset. The
	// service still boots; the endpoint just returns 503 until configured.
	ErrNotConfigured = errors.New("token exchange not configured")

	ErrMalformedAssertion = errors.New("malformed assertion")
	ErrBadSignature       = errors.New("assertion signature verification failed")
	ErrExpired            = errors.New("assertion expired")
	ErrInvalidIssuer      = errors.New("assertion issuer mismatch")
	ErrInvalidAudience    = errors.New("assertion audience mismatch")
	ErrMissingSubject     = errors.New("assertion missing subject")
	ErrUnknownRole        = errors.New("assertion role not recognized")
)

// Config configures an Exchanger. The signing secret / issuer / audience MUST
// match the wallet's existing auth middleware (configs AuthJWTSecret /
// AuthJWTIssuer / AuthJWTAudience) so that a minted token passes validation
// once AUTH_ENABLED is turned on.
type Config struct {
	// PayloadPublicKeyB64 is the RSA public key used to verify Payload's
	// assertions, provided as base64 of the PEM on a single line (Quave env
	// vars can't carry the multi-line PEM directly). It is base64-decoded and
	// then PEM-parsed. Empty, non-base64, or non-RSA ⇒ endpoint returns 503.
	PayloadPublicKeyB64 string

	// AccessTokenSecret is the HS256 secret the minted access token is signed
	// with. MUST equal the middleware's validation secret. Empty ⇒ 503.
	AccessTokenSecret string
	// AccessTokenIssuer / AccessTokenAudience are stamped onto the minted token
	// and MUST match what the middleware validates (may be empty, in which case
	// the middleware skips that check).
	AccessTokenIssuer   string
	AccessTokenAudience string
	// AccessTokenTTL is how long a minted access token is valid.
	AccessTokenTTL time.Duration

	// ExpectedAssertionIssuer / ExpectedAssertionAudience are the iss / aud the
	// Payload assertion must carry. Empty ⇒ that check is skipped.
	ExpectedAssertionIssuer   string
	ExpectedAssertionAudience string
}

// Exchanger verifies assertions and mints access tokens. Construct with New.
type Exchanger struct {
	cfg       Config
	publicKey *rsa.PublicKey // nil when unconfigured or PEM invalid
}

// Result is the outcome of a successful exchange.
type Result struct {
	Token     string
	ExpiresIn int // seconds
}

// New builds an Exchanger, parsing the RSA public key once. An empty or invalid
// PEM does not error: the Exchanger is simply left unconfigured (Configured()
// == false) so the service boots and the endpoint returns 503. This is
// deliberate — the public key is injected after deploy.
func New(cfg Config) *Exchanger {
	e := &Exchanger{cfg: cfg}
	if key, err := parsePublicKey(cfg.PayloadPublicKeyB64); err == nil {
		e.publicKey = key
	}
	return e
}

// parsePublicKey decodes a single-line base64 value into PEM bytes and parses
// them as an RSA public key. Any failure (empty, whitespace, bad base64, non-
// RSA PEM) returns an error so the Exchanger is simply left unconfigured — the
// service boots and the endpoint returns 503 rather than crash-looping.
func parsePublicKey(b64 string) (*rsa.PublicKey, error) {
	trimmed := strings.TrimSpace(b64)
	if trimmed == "" {
		return nil, errors.New("empty public key")
	}
	// Tolerate accidental internal whitespace/newlines in the base64 blob.
	trimmed = strings.Join(strings.Fields(trimmed), "")
	pemBytes, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("base64 decode public key: %w", err)
	}
	return jwt.ParseRSAPublicKeyFromPEM(pemBytes)
}

// Configured reports whether the exchange endpoint can serve requests. It needs
// a valid Payload public key AND a signing secret; without the secret a minted
// token could never be validated.
func (e *Exchanger) Configured() bool {
	return e != nil && e.publicKey != nil && e.cfg.AccessTokenSecret != ""
}

type assertionClaims struct {
	Role string `json:"role"`
	// FlowAddress is the caller's custodial Flow address, if Payload includes it
	// in the assertion. It is optional and backward-compatible: the front does
	// not send it yet, so an empty value is carried through unchanged.
	FlowAddress string `json:"flow_address"`
	jwt.RegisteredClaims
}

// Exchange verifies a Payload assertion and, on success, mints a wallet access
// token whose scopes are the wallet policy's mapping of the asserted role.
func (e *Exchanger) Exchange(assertion string) (*Result, error) {
	if !e.Configured() {
		return nil, ErrNotConfigured
	}
	if strings.TrimSpace(assertion) == "" {
		return nil, ErrMalformedAssertion
	}

	claims := assertionClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	token, err := parser.ParseWithClaims(assertion, &claims, func(t *jwt.Token) (any, error) {
		return e.publicKey, nil
	})
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, ErrExpired
		case errors.Is(err, jwt.ErrTokenSignatureInvalid):
			return nil, ErrBadSignature
		default:
			return nil, fmt.Errorf("%w: %v", ErrMalformedAssertion, err)
		}
	}
	if !token.Valid {
		return nil, ErrBadSignature
	}

	if e.cfg.ExpectedAssertionIssuer != "" && claims.Issuer != e.cfg.ExpectedAssertionIssuer {
		return nil, ErrInvalidIssuer
	}
	if e.cfg.ExpectedAssertionAudience != "" && !containsString(claims.Audience, e.cfg.ExpectedAssertionAudience) {
		return nil, ErrInvalidAudience
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return nil, ErrMissingSubject
	}

	scopes, ok := ScopesForRole(claims.Role)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownRole, claims.Role)
	}

	return e.mint(claims.Subject, claims.FlowAddress, scopes)
}

// mint produces an access token in the exact shape the wallet's auth middleware
// validates: HS256 with the shared secret, a space-separated `scope` claim, the
// subject carried through, and matching iss/aud/exp. The caller's flow_address
// (if the assertion carried one) is copied through unchanged so downstream
// guards can bind the token to an on-chain identity.
func (e *Exchanger) mint(subject, flowAddress string, scopes []string) (*Result, error) {
	now := time.Now()
	expiresAt := now.Add(e.cfg.AccessTokenTTL)

	registered := jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}
	if e.cfg.AccessTokenIssuer != "" {
		registered.Issuer = e.cfg.AccessTokenIssuer
	}
	if e.cfg.AccessTokenAudience != "" {
		registered.Audience = jwt.ClaimStrings{e.cfg.AccessTokenAudience}
	}

	claims := middleware.AuthClaims{
		Scope:            strings.Join(scopes, " "),
		FlowAddress:      flowAddress,
		RegisteredClaims: registered,
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(e.cfg.AccessTokenSecret))
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	return &Result{
		Token:     signed,
		ExpiresIn: int(e.cfg.AccessTokenTTL.Seconds()),
	}, nil
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
