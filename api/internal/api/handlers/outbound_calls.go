package handlers

import (
	"claimsio/internal/config"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/twilio/twilio-go"

	// "github.com/twilio/twilio-go/twiml"
	twilioApi "github.com/twilio/twilio-go/rest/api/v2010"

	"go.uber.org/zap"
)

// TODO - test this

type OutboundConversation struct {
	CallSid        string
	ConversationID string
	Number         string
	UserData       map[string]interface{}
}

var outboundConversations sync.Map

func HandleOutboundCall(cfg *config.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Number string `json:"number"`
			Prompt string `json:"prompt"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Number == "" {
			http.Error(w, "Phone number is required", http.StatusBadRequest)
			return
		}

		// Create Twilio call
		call, err := createTwilioCall(req.Number, req.Prompt, r.Host, cfg.TwilioPhoneNumber)
		if err != nil {
			zap.L().Error("Failed to create Twilio call", zap.Error(err))
			http.Error(w, "Failed to initiate call", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Call initiated",
			"callSid": call.Sid,
		})
	})
}

func HandleOutboundCallTwiml(cfg *config.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prompt := r.URL.Query().Get("prompt")
		number := r.URL.Query().Get("number")

		twiml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
        <Response>
            <Connect>
                <Stream url="wss://%s/outbound-media-stream">
                    <Parameter name="prompt" value="%s" />
                    <Parameter name="number" value="%s" />
                </Stream>
            </Connect>
        </Response>`, r.Host, url.QueryEscape(prompt), url.QueryEscape(number))

		w.Header().Set("Content-Type", "text/xml")
		w.Write([]byte(twiml))
	})
}

// from docs - can be improved
// router.POST("/answer", func(context *gin.Context) {
// 	say := &twiml.VoiceSay{
// 		Message: "Hello from your pals at Twilio! Have fun.",
// 	}

// 	twimlResult, err := twiml.Voice([]twiml.Element{say})
// 	if err != nil {
// 		context.String(http.StatusInternalServerError, err.Error())
// 	} else {
// 		context.Header("Content-Type", "text/xml")
// 		context.String(http.StatusOK, twimlResult)
// 	}
// })

func HandleOutboundMediaStream(cfg *config.Config, upgrader websocket.Upgrader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			zap.L().Error("Failed to upgrade connection", zap.Error(err))
			return
		}
		defer conn.Close()

		zap.L().Info("New outbound WebSocket connection established")

		var streamSid string
		var callSid string
		var elevenLabsWs *websocket.Conn
		var customParameters map[string]interface{}
		isDisconnecting := false

		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				zap.L().Error("Error reading message", zap.Error(err))
				break
			}

			if messageType != websocket.TextMessage {
				continue
			}

			var data map[string]interface{}
			if err := json.Unmarshal(message, &data); err != nil {
				zap.L().Error("Error parsing message", zap.Error(err))
				continue
			}

			event, ok := data["event"].(string)
			if !ok {
				continue
			}

			switch event {
			case "start":
				startData := data["start"].(map[string]interface{})
				streamSid = startData["streamSid"].(string)
				callSid = startData["callSid"].(string)
				customParameters = startData["customParameters"].(map[string]interface{})

				// check user data
				userData, err := checkUserExists(customParameters["number"].(string))
				if err != nil {
					zap.L().Error("Failed to check user", zap.Error(err))
					return
				}

				// init ElevenLabs
				elevenLabsWs, err = initializeElevenLabs(customParameters,
					userData,
					cfg.ElevenLabsAgentID,
					cfg.ElevenLabsAPIKey,
					streamSid,
				)
				if err != nil {
					zap.L().Error("Failed to initialize ElevenLabs", zap.Error(err))
					return
				}

				// store conversation data
				conv := &OutboundConversation{
					CallSid: callSid,
					Number:  customParameters["number"].(string),
				}
				outboundConversations.Store(callSid, conv)

			case "media":
				if elevenLabsWs != nil && !isDisconnecting {
					mediaData := data["media"].(map[string]interface{})
					payload := mediaData["payload"].(string)

					msg := map[string]interface{}{
						"user_audio_chunk": payload,
					}
					if err := elevenLabsWs.WriteJSON(msg); err != nil {
						zap.L().Error("Failed to send audio to ElevenLabs", zap.Error(err))
					}
				}

			case "stop":
				isDisconnecting = true
				if elevenLabsWs != nil {
					elevenLabsWs.WriteJSON(map[string]string{"type": "end_conversation"})
					elevenLabsWs.Close()
				}

				// Send final webhook
				if conv, ok := outboundConversations.Load(callSid); ok {
					payload := map[string]interface{}{
						"conversation_id": conv.(*OutboundConversation).ConversationID,
						"phone_number":    conv.(*OutboundConversation).Number,
						"call_sid":        conv.(*OutboundConversation).CallSid,
					}

					sendWebhook("outbound-calls", payload, cfg.N8NAuthToken)

					outboundConversations.Delete(callSid)
				}

				// Send disconnect signals
				conn.WriteJSON(map[string]interface{}{
					"event":     "mark_done",
					"streamSid": streamSid,
				})
				conn.WriteJSON(map[string]interface{}{
					"event":     "clear",
					"streamSid": streamSid,
				})
				return
			}
		}
	})
}

// private

func createTwilioCall(number, prompt, host, twilioPhoneNumber string) (*twilioApi.ApiV2010Call, error) {
	callURL := fmt.Sprintf("https://%s/outbound-call-twiml?prompt=%s&number=%s",
		host, url.QueryEscape(prompt), url.QueryEscape(number))

	client := twilio.NewRestClient()

	params := &twilioApi.CreateCallParams{}
	params.SetTo(number)
	params.SetFrom(twilioPhoneNumber)
	params.SetUrl(callURL)

	call, err := client.Api.CreateCall(params)
	if err != nil {
		return nil, fmt.Errorf("failed to create call: %v", err)
	}

	return call, nil
}
