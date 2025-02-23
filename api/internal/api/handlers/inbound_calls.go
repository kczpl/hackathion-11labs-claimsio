package handlers

import (
	"claimsio/internal/config"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"
)

// TODO - test this

type InboundConversation struct {
	StreamSid      string
	ConversationID string
	CallerPhone    string
	UserData       map[string]interface{}
}

var inboundConversations sync.Map

func HandleInboundCall(cfg *config.Config, upgrader websocket.Upgrader) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Failed to parse form", http.StatusBadRequest)
				return
			}

			callerPhone := r.FormValue("From")
			fmt.Printf("Incoming call received from: %s\n", callerPhone)

			// Check user authorization
			userData, err := checkUserExists(callerPhone)
			if err != nil || userData == nil {
				twiml := `<?xml version="1.0" encoding="UTF-8"?>
            <Response>
                <Say>Sorry, you are not authorized to make this call.</Say>
                <Hangup />
            </Response>`
				w.Header().Set("Content-Type", "text/xml")
				w.Write([]byte(twiml))
				return
			}

			// Generate TwiML for stream connection
			userDataStr, _ := json.Marshal(userData)
			twiml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
        <Response>
            <Connect>
                <Stream url="wss://%s/media-stream">
                    <Parameter name="caller_phone" value="%s" />
                    <Parameter name="user_data" value="%s" />
                </Stream>
            </Connect>
        </Response>`, r.Host, callerPhone, url.QueryEscape(string(userDataStr)))

			w.Header().Set("Content-Type", "text/xml")
			w.Write([]byte(twiml))
		})
}

func HandleInboundMediaStream(cfg *config.Config, upgrader websocket.Upgrader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Upgrading connection")
		conn, err := upgrader.Upgrade(w, r, nil)

		if err != nil {
			fmt.Printf("Failed to upgrade connection: %v\n", err)
			return
		}
		defer conn.Close()

		var streamSid string
		var elevenLabsWs *websocket.Conn
		isDisconnecting := false
		// Handle incoming messages
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("Error reading message: %v\n", err)
				break
			}
			if messageType != websocket.TextMessage {
				continue
			}

			var data map[string]interface{}
			if err := json.Unmarshal(message, &data); err != nil {
				fmt.Printf("Error parsing message: %v\n", err)
				continue
			}

			event, ok := data["event"].(string)
			if !ok {
				continue
			}

			// Skip non-stop events if disconnecting
			if isDisconnecting && event != "stop" {
				fmt.Printf("Ignoring event during disconnect: %s\n", event)
				continue
			}

			switch event {
			case "start":

				startData := data["start"].(map[string]interface{})
				streamSid = startData["streamSid"].(string)
				params := startData["customParameters"].(map[string]interface{})
				callerPhone := params["caller_phone"].(string)

				// Parse user data
				var userData map[string]interface{}
				if userDataStr, ok := params["user_data"].(string); ok {

					decodedStr, err := url.QueryUnescape(userDataStr)
					if err != nil {
						fmt.Printf("Failed to URL-decode user data: %v\n", err)
						return
					}

					fmt.Println("Decoded user data string: ", decodedStr)

					if err := json.Unmarshal([]byte(decodedStr), &userData); err != nil {
						fmt.Printf("Failed to parse user data: %v\n", err)
						return
					}
				}

				elevenLabsWs, err = initializeElevenLabs(params,
					userData,
					cfg.ElevenLabsAgentID,
					cfg.ElevenLabsAPIKey,
					streamSid,
				)
				if err != nil {
					fmt.Printf("Failed to initialize ElevenLabs: %v\n", err)
					return
				}
				// Store conversation data
				conv := &InboundConversation{
					StreamSid:   streamSid,
					CallerPhone: callerPhone,
					UserData:    userData,
				}
				inboundConversations.Store(streamSid, conv)

			case "media":
				if elevenLabsWs != nil && !isDisconnecting {
					mediaData := data["media"].(map[string]interface{})
					payload := mediaData["payload"].(string)

					// Forward audio to ElevenLabs
					msg := map[string]interface{}{
						"user_audio_chunk": payload,
					}
					if err := elevenLabsWs.WriteJSON(msg); err != nil {
						fmt.Printf("Failed to send audio to ElevenLabs: %v\n", err)
					}
					// else {
					// 	fmt.Printf("Successfully sent audio chunk to ElevenLabs for stream: %s\n", streamSid)
					// }
				}

			case "stop":
				isDisconnecting = true
				if elevenLabsWs != nil {
					elevenLabsWs.WriteJSON(map[string]string{"type": "end_conversation"})
					elevenLabsWs.Close()
				}

				// Send final webhook
				if conv, ok := inboundConversations.Load(streamSid); ok {
					payload := map[string]interface{}{
						"conversation_id": conv.(*InboundConversation).ConversationID,
						"phone_number":    conv.(*InboundConversation).CallerPhone,
						"call_sid":        conv.(*InboundConversation).StreamSid,
					}

					sendWebhook("inbound-calls", payload, cfg.N8NAuthToken)
					inboundConversations.Delete(streamSid)
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
				conn.WriteJSON(map[string]interface{}{
					"event":     "twiml",
					"streamSid": streamSid,
					"twiml":     "<Response><Hangup/></Response>",
				})
				return
			}
		}
	})
}
