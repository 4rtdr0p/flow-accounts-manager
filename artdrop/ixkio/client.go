// Package ixkio implements the ArtDrop wallet-api's client for Ixkio's Flex
// API "API mode" tap verification (see docs/CHIP-SIGNING-DESIGN.md §4a).
//
// Ixkio authenticates a physical NFC/RFID tap off-chain (the tag's AES-128
// SUN/CMAC key never touches this service or Cadence). Under model B the
// wallet-api custodies the chip's Flow account key, so before it ever wields
// that key to sign a challenge, it independently confirms a real tap
// happened by calling Ixkio itself — see the design doc §0 decision (a) for
// why the front cannot do this verification on the wallet-api's behalf.
//
// The Verifier interface is the swap point: dropping Ixkio later (e.g. once
// asymmetric self-signing chips arrive, §7 of the design doc — no verifier
// needed at all) or pointing at a different verification provider is a
// Go-level change behind this interface, with no Cadence and no on-chain
// change.
package ixkio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Tap is the raw evidence forwarded from a physical NTAG424 tap: the tag's
// public id (x), the scan counter in hex (n), and the per-tap authentication
// code (e). These three params are Ixkio's own tap-authentication protocol
// (docs.ixkio.com/flex-api) and are opaque to the wallet-api beyond passing
// them through untouched to Ixkio's API.
//
// Per design doc §4a, a given Tap is single-use at Ixkio (enforced by the
// scan counter n): callers must forward a tap unverified and let the
// wallet-api perform the one Verify call, never verify it twice.
type Tap struct {
	X string `json:"x"`
	N string `json:"n"`
	E string `json:"e"`
}

// Verifier confirms a Tap and reports the chip's Ixkio-side identifier
// (xuid) plus whether the tap passed.
//
// This is the swap point referenced by the design doc §0/§7: any type
// implementing Verify can stand in for the real Ixkio Client — a fake in
// tests, BypassVerifier for testnet, or eventually a different verification
// provider entirely — with zero change to callers.
type Verifier interface {
	Verify(ctx context.Context, tap Tap) (xuid string, pass bool, err error)
}

// verifyResponse is the JSON shape Ixkio's API-mode GET /v1/t returns. Per
// docs.ixkio.com/flex-api/flex-api-getting-started/flex-api-ntag424-authentication:
//
//	success: {"xuid":"q8w3sbcz","response":"Pass"}
//	fail:    {"xuid":"q8w3sbcz","response":"Fail"}
//	error:   {"xuid":"q8w3sbcz","error":"batch_inactive"}
type verifyResponse struct {
	Xuid     string `json:"xuid"`
	Response string `json:"response"`
	Error    string `json:"error"`
}

// Client calls the real Ixkio Flex API in "API mode" — the machine-readable
// Pass/Fail/error variant — as opposed to the redirect mode the front uses
// for the separate, unrelated consumer certificate-lookup flow
// (payload-galaxy-front's auth-bridge.ts). The API-mode response token (the
// "r" param) is NOT the same token as the front's redirect-mode
// IXKIO_RESPONSE_API_TOKEN; provisioning a wallet-api-owned API-mode token is
// a config/dashboard dependency, not a code one (design doc §4a).
type Client struct {
	apiURL        string
	responseToken string
	httpClient    *http.Client
}

// NewClient builds a real Ixkio Client. apiURL is normally
// "https://api.ixkio.com/v1/t" (ARTDROP_IXKIO_API_URL); responseToken is the
// "r" API-mode response token (ARTDROP_IXKIO_RESPONSE_TOKEN) — an empty
// token is allowed (simply omitted from the request), so a deployment can be
// brought up before Ixkio provisions one; real Verify calls will then fail
// against Ixkio's own API until it's set.
//
// httpClient may be nil, in which case http.DefaultClient is used.
func NewClient(apiURL, responseToken string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{apiURL: apiURL, responseToken: responseToken, httpClient: httpClient}
}

// Verify calls Ixkio's GET /v1/t?x=&n=&e=&r= and maps the response to
// (xuid, pass, err), matching design doc §4a exactly:
//   - response == "Pass" -> pass = true
//   - response == "Fail" -> pass = false, err = nil (a Fail is not itself a
//     Go error — it's information the caller acts on)
//   - a non-empty "error" field, or a non-2xx status, is a hard failure and
//     is NEVER treated as a Pass
//
// The request is sent with Cache-Control: no-store, mirroring the front's
// own `cache: 'no-store'` fetch option (auth-bridge.ts) — a cached Pass/Fail
// for a single-use tap counter would be actively wrong.
func (c *Client) Verify(ctx context.Context, tap Tap) (xuid string, pass bool, err error) {
	reqURL, err := url.Parse(c.apiURL)
	if err != nil {
		return "", false, fmt.Errorf("ixkio: invalid api url %q: %w", c.apiURL, err)
	}
	q := reqURL.Query()
	q.Set("x", tap.X)
	q.Set("n", tap.N)
	q.Set("e", tap.E)
	if c.responseToken != "" {
		q.Set("r", c.responseToken)
	}
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return "", false, fmt.Errorf("ixkio: building request: %w", err)
	}
	req.Header.Set("Cache-Control", "no-store")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("ixkio: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false, fmt.Errorf("ixkio: request failed with status %d", resp.StatusCode)
	}

	var out verifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", false, fmt.Errorf("ixkio: decoding response: %w", err)
	}

	if out.Error != "" {
		return out.Xuid, false, fmt.Errorf("ixkio: %s", out.Error)
	}

	return out.Xuid, out.Response == "Pass", nil
}

// BypassVerifier is a TESTNET-ONLY stand-in for Client that makes EVERY tap
// "Pass" WITHOUT ever calling Ixkio. It echoes the tap's X back as the xuid
// so downstream callers (the xuid -> chipId mapping) still have something to
// work with.
//
// THIS MUST NEVER BE ENABLED IN PRODUCTION: it removes the only check that a
// real physical tap happened, which is the entire security gate on wielding
// a chip's custodial private key (design doc §0/§6 — "the conjunction of a
// valid Ixkio Pass and the API scope" is the trust boundary; this collapses
// it to the scope alone). It exists solely so the create-escrow -> activate
// -> settle flow can be exercised end to end on testnet with no physical
// chip and no live Ixkio dependency (design doc §8 layer 4). Selected via
// ARTDROP_IXKIO_ENABLED=false — see NewVerifier.
type BypassVerifier struct{}

// Verify implements Verifier by unconditionally passing, echoing tap.X as
// the xuid, and making no network call whatsoever.
func (BypassVerifier) Verify(_ context.Context, tap Tap) (xuid string, pass bool, err error) {
	return tap.X, true, nil
}

// NewVerifier is the constructor callers (Phase 3, escrow activation) use to
// get the right Verifier for a deployment: the real Client when enabled is
// true, or the TESTNET-ONLY BypassVerifier when false. httpClient may be nil
// (see NewClient).
func NewVerifier(enabled bool, apiURL, responseToken string, httpClient *http.Client) Verifier {
	if !enabled {
		return BypassVerifier{}
	}
	return NewClient(apiURL, responseToken, httpClient)
}
