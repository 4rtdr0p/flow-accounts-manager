package studio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNoPaymentMethod is returned when a customer has no saved payment method
// to resolve as a default (no invoice_settings.default_payment_method and no
// payment methods on file).
var ErrNoPaymentMethod = errors.New("customer has no saved payment method")

// StripePaymentIntent is the subset of a Stripe PaymentIntent that the charge
// flow needs: its id and status.
type StripePaymentIntent struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// StripeClient creates and confirms PaymentIntents for Studio production
// charges. It is a minimal net/http client over the Stripe REST API; it never
// touches or returns key material.
type StripeClient struct {
	secretKey string
	baseURL   string
	http      *http.Client
}

// NewStripeClient creates a Stripe client. When secretKey is empty the client
// is disabled (CreateAndConfirm returns ErrStripeDisabled). baseURL defaults to
// the Stripe API; it is overridable in tests.
func NewStripeClient(secretKey, baseURL string) *StripeClient {
	if baseURL == "" {
		baseURL = "https://api.stripe.com/v1"
	}
	return &StripeClient{
		secretKey: secretKey,
		baseURL:   baseURL,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// CreateAndConfirm creates a PaymentIntent for the given amount (in cents) and
// immediately confirms it off-session against the customer's payment method.
// It returns the created intent. The idempotency key is used to make retries
// safe at the Stripe layer.
func (c *StripeClient) CreateAndConfirm(ctx context.Context, in StripeChargeInput) (*StripePaymentIntent, error) {
	if c == nil || c.secretKey == "" {
		return nil, ErrStripeDisabled
	}

	paymentMethodID := in.PaymentMethodID
	if paymentMethodID == "" {
		resolved, err := c.resolvePaymentMethod(ctx, in.CustomerID)
		if err != nil {
			return nil, err
		}
		paymentMethodID = resolved
	}

	form := url.Values{}
	form.Set("amount", fmt.Sprintf("%d", in.AmountCents))
	form.Set("currency", in.Currency)
	form.Set("customer", in.CustomerID)
	form.Set("confirm", "true")
	form.Set("off_session", "true")
	form.Set("payment_method", paymentMethodID)
	form.Set("automatic_payment_methods[enabled]", "true")
	if in.Metadata != "" {
		form.Set("metadata[charge_ref]", in.Metadata)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/payment_intents", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build stripe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+c.secretKey)
	if in.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", in.IdempotencyKey)
	}

	var intent StripePaymentIntent
	if err := c.do(req, &intent); err != nil {
		return nil, err
	}
	return &intent, nil
}

// resolvePaymentMethod returns the payment method to charge when the caller
// didn't supply one explicitly: the customer's invoice_settings.default_payment_method,
// falling back to the first payment method on file. It returns
// ErrNoPaymentMethod if the customer has neither.
func (c *StripeClient) resolvePaymentMethod(ctx context.Context, customerID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/customers/"+url.PathEscape(customerID), nil)
	if err != nil {
		return "", fmt.Errorf("build stripe customer request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.secretKey)

	var cust struct {
		InvoiceSettings struct {
			DefaultPaymentMethod string `json:"default_payment_method"`
		} `json:"invoice_settings"`
	}
	if err := c.do(req, &cust); err != nil {
		return "", fmt.Errorf("read stripe customer: %w", err)
	}
	if cust.InvoiceSettings.DefaultPaymentMethod != "" {
		return cust.InvoiceSettings.DefaultPaymentMethod, nil
	}

	q := url.Values{}
	q.Set("customer", customerID)
	q.Set("type", "card")
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/payment_methods?"+q.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("build stripe payment methods request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.secretKey)

	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.do(req, &list); err != nil {
		return "", fmt.Errorf("list stripe payment methods: %w", err)
	}
	if len(list.Data) == 0 {
		return "", fmt.Errorf("%w: customer %s", ErrNoPaymentMethod, customerID)
	}
	return list.Data[0].ID, nil
}

// do performs a Stripe request and decodes the JSON response. A non-2xx status
// is surfaced as a StripeError carrying the API error message.
func (c *StripeClient) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("stripe request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read stripe response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &apiErr)
		return fmt.Errorf("stripe API error (status %d): %s (type=%s code=%s)",
			resp.StatusCode, apiErr.Error.Message, apiErr.Error.Type, apiErr.Error.Code)
	}

	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode stripe response: %w", err)
		}
	}
	return nil
}

// StripeChargeInput is the data needed to create and confirm a PaymentIntent.
type StripeChargeInput struct {
	AmountCents     int64
	Currency        string
	CustomerID      string
	PaymentMethodID string
	IdempotencyKey  string
	Metadata        string
}
