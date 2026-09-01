package ixkio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestServer returns an httptest.Server that always writes body (raw JSON)
// with the given status code, regardless of the request.
func newTestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv
}

// TestClientVerifyPass covers the documented success shape
// {"xuid":"q8w3sbcz","response":"Pass"} mapping to pass=true with the xuid
// returned.
func TestClientVerifyPass(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, `{"xuid":"q8w3sbcz","response":"Pass"}`)

	c := NewClient(srv.URL, "test-token", nil)
	xuid, pass, err := c.Verify(context.Background(), Tap{X: "q8w3sbcz", N: "1a", E: "deadbeef"})
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if !pass {
		t.Fatal("expected pass=true for response=Pass")
	}
	if xuid != "q8w3sbcz" {
		t.Fatalf("expected xuid %q, got %q", "q8w3sbcz", xuid)
	}
}

// TestClientVerifyFail covers the documented failure shape
// {"xuid":"q8w3sbcz","response":"Fail"} mapping to pass=false with NO error
// (a Fail is information the caller acts on, not a Go error).
func TestClientVerifyFail(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, `{"xuid":"q8w3sbcz","response":"Fail"}`)

	c := NewClient(srv.URL, "test-token", nil)
	xuid, pass, err := c.Verify(context.Background(), Tap{X: "q8w3sbcz", N: "1a", E: "deadbeef"})
	if err != nil {
		t.Fatalf("Verify returned unexpected error: %v", err)
	}
	if pass {
		t.Fatal("expected pass=false for response=Fail")
	}
	if xuid != "q8w3sbcz" {
		t.Fatalf("expected xuid %q, got %q", "q8w3sbcz", xuid)
	}
}

// TestClientVerifyErrorBody covers the documented error shape
// {"xuid":"q8w3sbcz","error":"batch_inactive"} — must be a hard failure
// (non-nil err, pass=false), NEVER treated as a Pass.
func TestClientVerifyErrorBody(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, `{"xuid":"q8w3sbcz","error":"batch_inactive"}`)

	c := NewClient(srv.URL, "test-token", nil)
	_, pass, err := c.Verify(context.Background(), Tap{X: "q8w3sbcz", N: "1a", E: "deadbeef"})
	if err == nil {
		t.Fatal("expected an error for an error-body response")
	}
	if pass {
		t.Fatal("expected pass=false when the response body carries an error")
	}
}

// TestClientVerifyNonOKStatus covers a non-2xx HTTP status, which must be a
// hard failure regardless of body content.
func TestClientVerifyNonOKStatus(t *testing.T) {
	srv := newTestServer(t, http.StatusInternalServerError, `{"xuid":"q8w3sbcz","response":"Pass"}`)

	c := NewClient(srv.URL, "test-token", nil)
	_, pass, err := c.Verify(context.Background(), Tap{X: "q8w3sbcz", N: "1a", E: "deadbeef"})
	if err == nil {
		t.Fatal("expected an error for a non-2xx status")
	}
	if pass {
		t.Fatal("expected pass=false for a non-2xx status, even with a Pass body")
	}
}

// TestClientVerifySendsExpectedQueryParams confirms the request carries x, n,
// e and r (the response token) exactly as the design doc's GET
// /v1/t?x=&n=&e=&r= specifies, and that Cache-Control: no-store is set.
func TestClientVerifySendsExpectedQueryParams(t *testing.T) {
	var gotQuery, gotCacheControl string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotCacheControl = r.Header.Get("Cache-Control")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"xuid":"q8w3sbcz","response":"Pass"}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL, "test-token", nil)
	_, _, err := c.Verify(context.Background(), Tap{X: "q8w3sbcz", N: "1a", E: "deadbeef"})
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}

	if gotCacheControl != "no-store" {
		t.Fatalf("expected Cache-Control: no-store, got %q", gotCacheControl)
	}

	for _, want := range []string{"x=q8w3sbcz", "n=1a", "e=deadbeef", "r=test-token"} {
		if !containsParam(gotQuery, want) {
			t.Fatalf("expected query %q to contain %q", gotQuery, want)
		}
	}
}

func containsParam(rawQuery, kv string) bool {
	for _, part := range splitAmp(rawQuery) {
		if part == kv {
			return true
		}
	}
	return false
}

func splitAmp(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// TestBypassVerifierPassesWithoutHTTPCall covers the testnet bypass: with no
// server at all backing it, BypassVerifier must return (X, true, nil) for
// any tap, proving it makes no network call.
func TestBypassVerifierPassesWithoutHTTPCall(t *testing.T) {
	var v Verifier = BypassVerifier{}

	xuid, pass, err := v.Verify(context.Background(), Tap{X: "chip-abc123", N: "ff", E: "cafebabe"})
	if err != nil {
		t.Fatalf("BypassVerifier.Verify returned error: %v", err)
	}
	if !pass {
		t.Fatal("expected BypassVerifier to always pass")
	}
	if xuid != "chip-abc123" {
		t.Fatalf("expected xuid to echo tap.X %q, got %q", "chip-abc123", xuid)
	}
}

// TestNewVerifierSelectsBypassWhenDisabled confirms NewVerifier(false, ...)
// returns a Verifier that behaves like BypassVerifier (no HTTP call, always
// passes) even when given a bogus, unreachable API URL — proving disabled
// mode never touches the network.
func TestNewVerifierSelectsBypassWhenDisabled(t *testing.T) {
	v := NewVerifier(false, "http://127.0.0.1:1/unreachable", "unused-token", nil)

	xuid, pass, err := v.Verify(context.Background(), Tap{X: "chip-xyz", N: "01", E: "aa"})
	if err != nil {
		t.Fatalf("expected no error from bypass verifier, got: %v", err)
	}
	if !pass {
		t.Fatal("expected bypass verifier to pass")
	}
	if xuid != "chip-xyz" {
		t.Fatalf("expected xuid %q, got %q", "chip-xyz", xuid)
	}

	if _, ok := v.(BypassVerifier); !ok {
		t.Fatalf("expected NewVerifier(false, ...) to return a BypassVerifier, got %T", v)
	}
}

// TestNewVerifierSelectsClientWhenEnabled confirms NewVerifier(true, ...)
// returns a real *Client wired to the given URL/token, and that it actually
// performs the HTTP call.
func TestNewVerifierSelectsClientWhenEnabled(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, `{"xuid":"q8w3sbcz","response":"Pass"}`)

	v := NewVerifier(true, srv.URL, "test-token", nil)
	if _, ok := v.(*Client); !ok {
		t.Fatalf("expected NewVerifier(true, ...) to return a *Client, got %T", v)
	}

	xuid, pass, err := v.Verify(context.Background(), Tap{X: "q8w3sbcz", N: "1a", E: "deadbeef"})
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if !pass || xuid != "q8w3sbcz" {
		t.Fatalf("unexpected result: xuid=%q pass=%v", xuid, pass)
	}
}

// fakeVerifier is a minimal Verifier used to prove the interface is
// swappable independent of the real Client — e.g. a test double for a
// consumer that only depends on the Verifier interface.
type fakeVerifier struct {
	xuid string
	pass bool
	err  error
}

func (f fakeVerifier) Verify(_ context.Context, _ Tap) (string, bool, error) {
	return f.xuid, f.pass, f.err
}

// TestFakeVerifierSatisfiesInterface proves any type implementing Verify(...)
// can stand in for Client behind the Verifier interface, which is the whole
// point of the swap-point design (design doc §0/§7: dropping Ixkio later is a
// Go-level change behind this interface).
func TestFakeVerifierSatisfiesInterface(t *testing.T) {
	var v Verifier = fakeVerifier{xuid: "fake-xuid", pass: true}

	xuid, pass, err := v.Verify(context.Background(), Tap{X: "irrelevant"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pass || xuid != "fake-xuid" {
		t.Fatalf("unexpected result: xuid=%q pass=%v", xuid, pass)
	}
}
