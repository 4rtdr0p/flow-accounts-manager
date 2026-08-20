package studio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newStripeTestServer wires a mux that answers the three endpoints
// CreateAndConfirm and resolvePaymentMethod call, returning the given
// customer/payment-methods/payment-intent bodies. It also records the
// payment_method form value the payment_intents call was made with.
func newStripeTestServer(t *testing.T, customerBody, paymentMethodsBody string) (*httptest.Server, *string) {
	t.Helper()
	var gotPaymentMethod string
	mux := http.NewServeMux()
	mux.HandleFunc("/customers/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(customerBody))
	})
	mux.HandleFunc("/payment_methods", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(paymentMethodsBody))
	})
	mux.HandleFunc("/payment_intents", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotPaymentMethod = r.PostForm.Get("payment_method")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pi_123","status":"succeeded"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &gotPaymentMethod
}

func validStripeChargeInput() StripeChargeInput {
	return StripeChargeInput{
		AmountCents: 2500,
		Currency:    "usd",
		CustomerID:  "cus_123",
	}
}

func TestCreateAndConfirmExplicitPaymentMethodIsHonoured(t *testing.T) {
	// A non-empty PaymentMethodID must be used unchanged; the customer and
	// payment methods endpoints must never be consulted.
	mux := http.NewServeMux()
	mux.HandleFunc("/customers/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("customer lookup must not happen when PaymentMethodID is explicit")
	})
	mux.HandleFunc("/payment_methods", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("payment methods listing must not happen when PaymentMethodID is explicit")
	})
	var gotPaymentMethod string
	mux.HandleFunc("/payment_intents", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotPaymentMethod = r.PostForm.Get("payment_method")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pi_123","status":"succeeded"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := NewStripeClient("sk_test", srv.URL)
	in := validStripeChargeInput()
	in.PaymentMethodID = "pm_explicit"

	if _, err := c.CreateAndConfirm(context.Background(), in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPaymentMethod != "pm_explicit" {
		t.Fatalf("expected payment_method pm_explicit, got %q", gotPaymentMethod)
	}
}

func TestCreateAndConfirmResolvesDefaultFromInvoiceSettings(t *testing.T) {
	srv, gotPaymentMethod := newStripeTestServer(t,
		`{"id":"cus_123","invoice_settings":{"default_payment_method":"pm_default"}}`,
		`{"data":[]}`,
	)
	c := NewStripeClient("sk_test", srv.URL)

	if _, err := c.CreateAndConfirm(context.Background(), validStripeChargeInput()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *gotPaymentMethod != "pm_default" {
		t.Fatalf("expected payment_method pm_default, got %q", *gotPaymentMethod)
	}
}

func TestCreateAndConfirmFallsBackToFirstPaymentMethod(t *testing.T) {
	srv, gotPaymentMethod := newStripeTestServer(t,
		`{"id":"cus_123","invoice_settings":{"default_payment_method":null}}`,
		`{"data":[{"id":"pm_first"},{"id":"pm_second"}]}`,
	)
	c := NewStripeClient("sk_test", srv.URL)

	if _, err := c.CreateAndConfirm(context.Background(), validStripeChargeInput()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *gotPaymentMethod != "pm_first" {
		t.Fatalf("expected payment_method pm_first, got %q", *gotPaymentMethod)
	}
}

func TestCreateAndConfirmNoPaymentMethodFails(t *testing.T) {
	srv, _ := newStripeTestServer(t,
		`{"id":"cus_123","invoice_settings":{"default_payment_method":null}}`,
		`{"data":[]}`,
	)
	c := NewStripeClient("sk_test", srv.URL)

	_, err := c.CreateAndConfirm(context.Background(), validStripeChargeInput())
	if !errors.Is(err, ErrNoPaymentMethod) {
		t.Fatalf("expected ErrNoPaymentMethod, got %v", err)
	}
	if !strings.Contains(err.Error(), "cus_123") {
		t.Fatalf("expected error to name the customer, got %q", err.Error())
	}
}

// newMetadataCaptureServer wires a mux that only answers /payment_intents and
// records the full posted form, so tests can inspect every metadata[...]
// field the request carried. Tests using it must supply an explicit
// PaymentMethodID so CreateAndConfirm never needs the customer/payment
// methods endpoints (unregistered here).
func newMetadataCaptureServer(t *testing.T) (*httptest.Server, *url.Values) {
	t.Helper()
	var got url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/payment_intents", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		got = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pi_123","status":"succeeded"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestCreateAndConfirmExpandsJSONMetadataIntoSeparateKeys(t *testing.T) {
	srv, gotForm := newMetadataCaptureServer(t)
	c := NewStripeClient("sk_test", srv.URL)

	in := validStripeChargeInput()
	in.PaymentMethodID = "pm_explicit"
	in.Metadata = `{"editionId":"ed_1","artistProfileId":"ap_1","type":"production"}`

	if _, err := c.CreateAndConfirm(context.Background(), in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for key, want := range map[string]string{"editionId": "ed_1", "artistProfileId": "ap_1", "type": "production"} {
		if got := gotForm.Get("metadata[" + key + "]"); got != want {
			t.Errorf("expected metadata[%s]=%q, got %q", key, want, got)
		}
	}
	if gotForm.Get("metadata[charge_ref]") != "" {
		t.Errorf("expected no fallback charge_ref field when metadata expands, got %q", gotForm.Get("metadata[charge_ref]"))
	}
}

func TestCreateAndConfirmFallsBackToChargeRefOnNonJSONMetadata(t *testing.T) {
	srv, gotForm := newMetadataCaptureServer(t)
	c := NewStripeClient("sk_test", srv.URL)

	in := validStripeChargeInput()
	in.PaymentMethodID = "pm_explicit"
	in.Metadata = "not-json-just-a-ref-123"

	if _, err := c.CreateAndConfirm(context.Background(), in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gotForm.Get("metadata[charge_ref]"); got != in.Metadata {
		t.Errorf("expected metadata[charge_ref]=%q, got %q", in.Metadata, got)
	}
}

func TestCreateAndConfirmFallsBackToChargeRefOnJSONArrayMetadata(t *testing.T) {
	// A JSON array is valid JSON but not an object: it must fall back rather
	// than being treated as expandable key/value metadata.
	srv, gotForm := newMetadataCaptureServer(t)
	c := NewStripeClient("sk_test", srv.URL)

	in := validStripeChargeInput()
	in.PaymentMethodID = "pm_explicit"
	in.Metadata = `["ed_1","ap_1"]`

	if _, err := c.CreateAndConfirm(context.Background(), in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gotForm.Get("metadata[charge_ref]"); got != in.Metadata {
		t.Errorf("expected metadata[charge_ref]=%q, got %q", in.Metadata, got)
	}
}

func TestCreateAndConfirmMetadataTruncatesLongValue(t *testing.T) {
	srv, gotForm := newMetadataCaptureServer(t)
	c := NewStripeClient("sk_test", srv.URL)

	longValue := strings.Repeat("x", 600)
	in := validStripeChargeInput()
	in.PaymentMethodID = "pm_explicit"
	in.Metadata = fmt.Sprintf(`{"note":%q}`, longValue)

	if _, err := c.CreateAndConfirm(context.Background(), in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := gotForm.Get("metadata[note]")
	if len(got) != stripeMetadataMaxValueChars {
		t.Fatalf("expected value truncated to %d chars, got %d", stripeMetadataMaxValueChars, len(got))
	}
	if got != longValue[:stripeMetadataMaxValueChars] {
		t.Fatalf("expected truncated value to be a prefix of the original")
	}
}

func TestCreateAndConfirmMetadataDropsKeysBeyondLimit(t *testing.T) {
	srv, gotForm := newMetadataCaptureServer(t)
	c := NewStripeClient("sk_test", srv.URL)

	obj := make(map[string]string, stripeMetadataMaxKeys+5)
	for i := 0; i < stripeMetadataMaxKeys+5; i++ {
		obj[fmt.Sprintf("key%03d", i)] = "v"
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	in := validStripeChargeInput()
	in.PaymentMethodID = "pm_explicit"
	in.Metadata = string(raw)

	if _, err := c.CreateAndConfirm(context.Background(), in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for key := range *gotForm {
		if strings.HasPrefix(key, "metadata[key") {
			count++
		}
	}
	if count != stripeMetadataMaxKeys {
		t.Fatalf("expected exactly %d metadata keys kept, got %d", stripeMetadataMaxKeys, count)
	}
	// The rule is deterministic: the lexicographically first N keys survive.
	if gotForm.Get("metadata[key000]") != "v" {
		t.Errorf("expected key000 (sorted first) to survive truncation")
	}
	if gotForm.Get(fmt.Sprintf("metadata[key%03d]", stripeMetadataMaxKeys+4)) != "" {
		t.Errorf("expected the lexicographically last key to be dropped")
	}
}
