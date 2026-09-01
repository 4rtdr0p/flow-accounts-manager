package purchase

import (
	"context"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newPythTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func TestPythClientSendsAPIKey(t *testing.T) {
	server := newPythTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		if strings.Contains(r.URL.String(), "test-key") {
			t.Errorf("API key leaked into URL: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"parsed":[{"price":{"price":"2500000000","expo":-8,"publish_time":1893500000}}]}`))
	}))

	client := NewPythClient(server.URL, "feed", "test-key", time.Minute)
	price, err := client.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(price.PriceUSD-25) > 1e-9 {
		t.Fatalf("PriceUSD = %v, want 25", price.PriceUSD)
	}
}

func TestPythClientWithoutAPIKey(t *testing.T) {
	server := newPythTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"parsed":[{"price":{"price":"2500000000","expo":-8,"publish_time":1893500000}}]}`))
	}))

	if _, err := NewPythClient(server.URL, "feed", "", time.Minute).Latest(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPythClientPropagatesUnauthorized(t *testing.T) {
	server := newPythTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))

	_, err := NewPythClient(server.URL, "feed", "bad-key", time.Minute).Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("error = %v, want status 401", err)
	}
}
