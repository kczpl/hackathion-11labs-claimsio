package api

import (
	"net/http"

	h "claimsio/internal/api/handlers"
	"claimsio/internal/config"
	"claimsio/internal/middleware"

	"github.com/gorilla/websocket"
)

func NewRouter(cfg *config.Config, upgrader websocket.Upgrader) http.Handler {
	mux := http.NewServeMux()

	// Create handler dependencies
	// middleware
	// apiHandler = middleware.Logging(apiHandler)

	// Register routes
	// mux.HandleFunc("/up", h.HealthCheck)

	// Twilio x ElevenLabs
	mux.Handle("/incoming-call-eleven", h.HandleInboundCall(cfg, upgrader))
	mux.Handle("/outbound-call", h.HandleOutboundCall(cfg))
	mux.Handle("/outbound-call-twiml", h.HandleOutboundCallTwiml(cfg))
	mux.Handle("/media-stream", h.HandleInboundMediaStream(cfg, upgrader))
	mux.Handle("/outbound-media-stream", h.HandleOutboundMediaStream(cfg, upgrader))

	// Stripe
	mux.Handle("/payment-link", h.HandleCreatePaymentLink(cfg))

	// Twilio
	mux.Handle("/send-sms", h.HandleSendSMS(cfg))

	// Prompts
	mux.HandleFunc("/prompts/", h.HandleGetPromptByNameParam) // Note the trailing slash

	var handler http.Handler = mux
	handler = middleware.Logging(handler)

	return handler
}
