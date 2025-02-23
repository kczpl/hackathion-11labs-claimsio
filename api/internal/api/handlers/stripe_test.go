package handlers

import (
	"bytes"
	"claimsio/internal/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stripe/stripe-go/v72"
	"github.com/stripe/stripe-go/v72/form"
)

type MockBackend struct{}

func (mb *MockBackend) Call(method, path, key string, params stripe.ParamsContainer, v stripe.LastResponseSetter) error {
	switch path {
	case "/v1/products":
		*(v.(*stripe.Product)) = stripe.Product{ID: "prod_1234567890"}
	case "/v1/prices":
		*(v.(*stripe.Price)) = stripe.Price{ID: "price_1234567890"}
	case "/v1/payment_links":
		*(v.(*stripe.PaymentLink)) = stripe.PaymentLink{
			ID:  "plink_1234567890",
			URL: "https://stripe.com/pay/cs_test_1234567890",
		}
	}
	return nil
}

func (mb *MockBackend) CallRaw(method, path, key string, body *form.Values, params *stripe.Params, v stripe.LastResponseSetter) error {
	return nil
}

func (mb *MockBackend) CallMultipart(method, path, key, boundary string, body *bytes.Buffer, params *stripe.Params, v stripe.LastResponseSetter) error {
	return nil
}

func (mb *MockBackend) SetMaxNetworkRetries(maxNetworkRetries int64) {}

func (mb *MockBackend) CallStreaming(method, path, key string, params stripe.ParamsContainer, v stripe.StreamingLastResponseSetter) error {
	return nil
}

func TestHandleCreatePaymentLink(t *testing.T) {
	// Mock config
	cfg := &config.Config{
		StripeAPIKeyTest: "sk_test_1234567890",
	}

	// Mock Stripe API calls
	mockBackend := &MockBackend{}

	// Set the mock backend for the API
	stripe.SetBackend(stripe.APIBackend, mockBackend)
	defer stripe.SetBackend(stripe.APIBackend, nil)

	// Create request
	reqBody := PaymentLinkRequest{
		Amount:      100.50,
		DebtorID:    "debtor123",
		CaseID:      "case456",
		Currency:    "usd",
		Environment: "test",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/create-payment-link", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	// Create response recorder
	rr := httptest.NewRecorder()

	// Call the handler
	handler := HandleCreatePaymentLink(cfg)
	handler.ServeHTTP(rr, req)

	// Check status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// Check response body
	var resp PaymentLinkResponse
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	if err != nil {
		t.Errorf("failed to unmarshal response: %v", err)
	}

	expectedURL := "https://stripe.com/pay/cs_test_1234567890"
	if resp.PaymentURL != expectedURL {
		t.Errorf("unexpected payment URL: got %v want %v", resp.PaymentURL, expectedURL)
	}

	expectedID := "plink_1234567890"
	if resp.PaymentLinkID != expectedID {
		t.Errorf("unexpected payment link ID: got %v want %v", resp.PaymentLinkID, expectedID)
	}
}
