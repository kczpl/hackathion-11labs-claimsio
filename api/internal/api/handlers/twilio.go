package handlers

import (
	"encoding/json"
	"net/http"

	"claimsio/internal/config"

	twilio "github.com/twilio/twilio-go"
	openapi "github.com/twilio/twilio-go/rest/api/v2010"
)

type SMSRequest struct {
	To      string `json:"to"`
	Message string `json:"message"`
}

type SMSResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	SID     string `json:"sid,omitempty"`
}

func HandleSendSMS(cfg *config.Config) http.HandlerFunc {
	// initialize twilio client
	client := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: cfg.TwilioAccountSID,
		Password: cfg.TwilioAuthToken,
	})

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// decode request body
		var req SMSRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// validate request
		if req.To == "" || req.Message == "" {
			http.Error(w, "Missing required fields", http.StatusBadRequest)
			return
		}

		// prepare twilio params
		params := &openapi.CreateMessageParams{}
		params.SetTo(req.To)
		params.SetFrom(cfg.TwilioPhoneNumber)
		params.SetBody(req.Message)

		// send sms
		resp, err := client.Api.CreateMessage(params)
		if err != nil {
			http.Error(w, "Failed to send SMS", http.StatusInternalServerError)
			return
		}

		// prepare response
		response := SMSResponse{
			Success: true,
			Message: "SMS sent successfully",
			SID:     *resp.Sid,
		}

		// send response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}
