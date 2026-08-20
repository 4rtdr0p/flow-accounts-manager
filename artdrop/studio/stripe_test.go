package studio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
