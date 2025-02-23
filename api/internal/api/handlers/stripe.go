package handlers

import (
	"claimsio/internal/config"
	"encoding/json"
	"fmt"
	"math"
	"net/http"

	"github.com/stripe/stripe-go/v72"
	"github.com/stripe/stripe-go/v72/paymentlink"
	"github.com/stripe/stripe-go/v72/price"
	"github.com/stripe/stripe-go/v72/product"
)

// Test it
type PaymentLinkRequest struct {
	Amount      float64 `json:"amount"`
	DebtorID    string  `json:"debtor_id"`
	CaseID      string  `json:"case_id"`
	Currency    string  `json:"currency"`
	Environment string  `json:"environment"`
}

type PaymentLinkResponse struct {
	PaymentURL    string `json:"payment_url"`
	PaymentLinkID string `json:"payment_link_id"`
	CaseID        string `json:"case_id"`
}

func HandleCreatePaymentLink(cfg *config.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// parse input parameters
		var params PaymentLinkRequest

		if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
			writeErrorResponse(w, http.StatusBadRequest, "invalid request parameters", err)
			return
		}

		// set stripe api key based on environment
		if params.Environment == "production" {
			stripe.Key = cfg.StripeAPIKeyLive
		} else {
			stripe.Key = cfg.StripeAPIKeyTest
		}

		// create product
		productParams := &stripe.ProductParams{
			Name: stripe.String(fmt.Sprintf("Debt payment")), //, params.DebtorID[:5], params.CaseID[:5])),
		}

		prod, err := product.New(productParams)
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "failed to create stripe product", err)
			return
		}

		// create price
		amount := int64(math.Round(params.Amount * 100))
		priceParams := &stripe.PriceParams{
			Currency:   stripe.String(params.Currency),
			Product:    stripe.String(prod.ID),
			UnitAmount: stripe.Int64(amount),
		}
		p, err := price.New(priceParams)
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "failed to create stripe price", err)
			return
		}

		// create payment link
		linkParams := &stripe.PaymentLinkParams{
			LineItems: []*stripe.PaymentLinkLineItemParams{
				{
					Price:    stripe.String(p.ID),
					Quantity: stripe.Int64(1),
				},
			},
			PaymentMethodTypes: stripe.StringSlice([]string{
				"blik",
				"p24",
				"card",
			}),
			AfterCompletion: &stripe.PaymentLinkAfterCompletionParams{
				Type: stripe.String("redirect"),
				Redirect: &stripe.PaymentLinkAfterCompletionRedirectParams{
					URL: stripe.String("https://pay.claimsio.com/dashboard"),
				},
			},
		}

		linkParams.AddMetadata("debtor_id", params.DebtorID)
		linkParams.AddMetadata("case_id", params.CaseID)

		link, err := paymentlink.New(linkParams)
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "failed to create payment link", err)
			return
		}

		// // // save payment link to supabase cases table
		// updateQuery := fmt.Sprintf(`
		// 	UPDATE cases
		// 	SET payment_link_url = '%s'
		// 	WHERE case_id = '%s'
		// `, link.URL, params.CaseID)

		// write success response
		writeJSON(w, http.StatusOK, PaymentLinkResponse{
			CaseID:        params.CaseID,
			PaymentURL:    link.URL,
			PaymentLinkID: link.ID,
		})
	})
}

// helper functions
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeErrorResponse(w http.ResponseWriter, status int, message string, err error) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{
		Error: fmt.Sprintf("%s: %v", message, err),
	})
}

func handleStripeError(w http.ResponseWriter, err error) {
	if stripeErr, ok := err.(*stripe.Error); ok {
		switch stripeErr.Type {
		case stripe.ErrorTypeCard:
			http.Error(w, stripeErr.Error(), http.StatusBadRequest)
		case stripe.ErrorTypeInvalidRequest:
			http.Error(w, stripeErr.Error(), http.StatusBadRequest)
		default:
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
