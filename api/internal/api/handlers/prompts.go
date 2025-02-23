package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"claimsio/internal/ai"
)

type InboundCallPromptParams struct {
	Name         string `json:"name"`
	CaseNumber   string `json:"case_number"`
	DebtAmount   int64  `json:"debt_amount"`
	Currency     string `json:"currency"`
	Phone        string `json:"phone"`
	PrevMessages string `json:"prev_messages"`
}

type OutboundCallPromptParams struct {
	Name         string `json:"name"`
	Language     string `json:"language"`
	CaseNumber   string `json:"case_number"`
	DebtAmount   int64  `json:"debt_amount"`
	Currency     string `json:"currency"`
	Phone        string `json:"phone"`
	PrevMessages string `json:"prev_messages"`
}

type InitialMessagePromptParams struct {
	Name        string `json:"name"`
	Language    string `json:"language"`
	CaseNumber  string `json:"case_number"`
	DebtAmount  int64  `json:"debt_amount"`
	Currency    string `json:"currency"`
	Phone       string `json:"phone"`
	Description string `json:"description"`
}

func HandleGetPromptByNameParam(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// extract prompt name from URL path
	path := strings.TrimPrefix(r.URL.Path, "/prompts/")
	if path == "" {
		http.Error(w, "Prompt name is required", http.StatusBadRequest)
		return
	}

	switch path {
	case "inbound-call":
		var params InboundCallPromptParams
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		prompt, err := ai.GenerateInboundCallPrompt(
			params.Name,
			params.CaseNumber,
			params.DebtAmount,
			params.Currency,
			params.Phone,
			params.PrevMessages,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		systemPrompt := ai.GetSystemPrompt()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"system_prompt": systemPrompt,
			"prompt":        prompt,
		})
		return
	case "outbound-call":
		var params OutboundCallPromptParams
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		prompt, err := ai.GenerateOutboundCallPrompt(
			params.Name,
			params.CaseNumber,
			params.DebtAmount,
			params.Currency,
			params.Phone,
			params.PrevMessages,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		systemPrompt := ai.GetSystemPrompt()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"system_prompt": systemPrompt,
			"prompt":        prompt,
		})
		return
	case "init-message":
		var params InitialMessagePromptParams
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		systemPrompt := ai.GetSystemPrompt()

		prompt, err := ai.GenerateInitMessagePrompt(
			params.Name,
			params.CaseNumber,
			params.DebtAmount,
			params.Currency,
			params.Phone,
			params.Language,
			params.Description,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"system_prompt": systemPrompt,
			"prompt": prompt,
		})
		return
	}
}
