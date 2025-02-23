package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port                string
	SupabaseServiceRole string
	SupabasePgURL      string
	ElevenLabsAPIKey    string
	ElevenLabsAgentID   string
	TwilioAccountSID    string
	TwilioAuthToken     string
	TwilioPhoneNumber   string
	N8NAuthToken        string
	Environment         string
	StripeAPIKeyLive    string
	StripeAPIKeyTest    string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                getEnv("PORT", "8000"),
		SupabaseServiceRole: getEnv("SUPABASE_SERVICE_ROLE", ""),
		SupabasePgURL:       getEnv("SUPABASE_PG_URL", ""),
		ElevenLabsAPIKey:    getEnv("ELEVENLABS_API_KEY", ""),
		ElevenLabsAgentID:   getEnv("ELEVENLABS_AGENT_ID", ""),
		TwilioAccountSID:    getEnv("TWILIO_ACCOUNT_SID", ""),
		TwilioAuthToken:     getEnv("TWILIO_AUTH_TOKEN", ""),
		TwilioPhoneNumber:   getEnv("TWILIO_PHONE_NUMBER", ""),
		N8NAuthToken:        getEnv("N8N_AUTH_TOKEN", ""),
		Environment:         getEnv("ENV", "development"),
		StripeAPIKeyLive:    getEnv("STRIPE_API_KEY_LIVE", ""),
		StripeAPIKeyTest:    getEnv("STRIPE_API_KEY_TEST", "sk_test"),
	}

	// Validate required environment variables
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}
func (c *Config) validate() error {
	required := map[string]string{
		// "ELEVENLABS_API_KEY":  c.ElevenLabsAPIKey,
		// "ELEVENLABS_AGENT_ID": c.ElevenLabsAgentID,
		// "N8N_AUTH_TOKEN":      c.N8NAuthToken,
	}

	for name, value := range required {
		if value == "" {
			return fmt.Errorf("missing required environment variable: %s", name)
		}
	}

	return nil
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
