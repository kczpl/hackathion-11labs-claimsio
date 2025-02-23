package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/websocket"
)

type ElevenLabsConfig struct {
	Type                       string `json:"type"`
	ConversationConfigOverride struct {
		Agent struct {
			Prompt struct {
				Prompt string `json:"prompt"`
			} `json:"prompt"`
			FirstMessage string `json:"first_message"`
		} `json:"agent"`
	} `json:"conversation_config_override"`
	ClientData struct {
		DynamicVariables map[string]string `json:"dynamic_variables,omitempty"`
	} `json:"client_data,omitempty"`
}

func initializeElevenLabs(
	params map[string]interface{},
	userData map[string]interface{},
	agentID string,
	apiKey string,
	streamSid string,
) (*websocket.Conn, error) {
	signedURL, err := getElevenLabsSignedURL(agentID, apiKey)
	if err != nil {
		return nil, err
	}

	ws, _, err := websocket.DefaultDialer.Dial(signedURL, nil)
	if err != nil {
		return nil, err
	}

	config := createElevenLabsConfig(params, userData)

	if err := ws.WriteJSON(config); err != nil {
		ws.Close()
		return nil, err
	}

	// Handle ElevenLabs messages in a separate goroutine
	// go handleElevenLabsMessages(ws)
	fmt.Println("Handling ElevenLabs messages")
	// go handleElevenLabsMessages(ws, streamSid)
	go handleElevenLabsMessages(ws, streamSid)

	return ws, nil
}

func handleElevenLabsMessages(ws *websocket.Conn, streamSid string) {
	fmt.Println("Handling ElevenLabs messages")
	fmt.Println("Stream SID: ", streamSid)
	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			fmt.Printf("Error reading from ElevenLabs: %v\n", err)
			return
		}

		var data map[string]interface{}
		if err := json.Unmarshal(message, &data); err != nil {
			fmt.Printf("Error parsing ElevenLabs message: %v\n", err)
			continue
		}

		messageType, ok := data["type"].(string)
		if !ok {
			continue
		}

		switch messageType {
		case "audio":
			fmt.Println("Audio event received from ElevenLabs")
			if audioEvent, ok := data["audio_event"].(map[string]interface{}); ok {
				if audioBase64, ok := audioEvent["audio_base_64"].(string); ok {
					audioData := map[string]interface{}{
						"event":     "media",
						"streamSid": streamSid,
						"media": map[string]interface{}{
							"payload": audioBase64,
						},
					}
					if err := ws.WriteJSON(audioData); err != nil {
						fmt.Printf("Error forwarding audio to Twilio: %v\n", err)
					} else {
						fmt.Printf("Successfully forwarded audio chunk to Twilio for stream: %s\n", streamSid)
					}
				}
			}

		case "conversation_initiation_metadata":
			fmt.Println("Conversation initiation metadata event received from ElevenLabs")
			if metadata, ok := data["conversation_initiation_metadata_event"].(map[string]interface{}); ok {
				if conversationID, ok := metadata["conversation_id"].(string); ok {
					// Store conversation ID
					if conv, exists := inboundConversations.Load(streamSid); exists {
						conv.(*InboundConversation).ConversationID = conversationID
						inboundConversations.Store(streamSid, conv)
					}
				}
			}

		case "interruption":
			ws.WriteJSON(map[string]interface{}{
				"event": "clear",
			})

		case "ping":
			if pingEvent, ok := data["ping_event"].(map[string]interface{}); ok {
				if eventID, ok := pingEvent["event_id"].(string); ok {
					ws.WriteJSON(map[string]interface{}{
						"type":     "pong",
						"event_id": eventID,
					})
				}
			}
		}
	}
}

func createElevenLabsConfig(params map[string]interface{}, userData map[string]interface{}) ElevenLabsConfig {
	config := ElevenLabsConfig{
		Type: "conversation_initiation_client_data",
	}

	if userData != nil {
		// extract debtor_id from userData
		debtorID, _ := userData["debtor_id"].(string)

		// extract caller_phone from params
		callerPhone, _ := params["caller_phone"].(string)

		// get prompt from params if exists
		prompt := ""
		if p, ok := params["prompt"].(string); ok {
			prompt = p
		}

		// create base prompt with available information
		basePrompt := fmt.Sprintf(`You are a customer service representative AI agent.
Context about the call:
Debtor ID: %s
Caller Phone: %s`, debtorID, callerPhone)

		if prompt != "" {
			basePrompt = fmt.Sprintf("%s\n\n%s", basePrompt, prompt)
		}

		config.ConversationConfigOverride.Agent.Prompt.Prompt = basePrompt
		config.ConversationConfigOverride.Agent.FirstMessage = "Hello, do you have a moment to talk?"

		// set dynamic variables with available data
		config.ClientData.DynamicVariables = map[string]string{
			"caller_phone": callerPhone,
			"debtor_id":    debtorID,
		}
	} else {
		// default configuration for unauthorized users
		config.ConversationConfigOverride.Agent.Prompt.Prompt = "You are a customer service representative"
		config.ConversationConfigOverride.Agent.FirstMessage = "Hello, do you have a moment to talk?"
	}

	return config
}

func sendWebhook(endpoint string, payload map[string]interface{}, authToken string) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling webhook payload: %v", err)
	}

	url := fmt.Sprintf("http://app-n8n-1:5678/webhook/%s", endpoint)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating webhook request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error sending webhook: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func getElevenLabsSignedURL(agentID string, apiKey string) (string, error) {
	url := fmt.Sprintf("https://api.elevenlabs.io/v1/convai/conversation/get_signed_url?agent_id=%s",
		agentID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("xi-api-key", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get signed URL: %s", resp.Status)
	}

	var result struct {
		SignedURL string `json:"signed_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.SignedURL, nil
}

func checkUserExists(phone string) (map[string]interface{}, error) {
	payload := map[string]string{"phone": phone}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", "http://app-n8n-1:5678/webhook/check-user", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user check failed with status: %d", resp.StatusCode)
	}

	var userData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&userData); err != nil {
		return nil, err
	}

	return userData, nil
}
