package recommender

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the recommendation service.
type Config struct {
	// Server connection
	ServerURL   string
	APIKey      string

	// Sync behavior
	SyncInterval    time.Duration
	HTTPRequestTimeout time.Duration
	RetryMaxAttempts int
	RetryBaseDelay   time.Duration

	// Collection behavior
	CollectionPrefix string  // prefix for collection names, e.g. "★ " to namespace
	DryRun           bool    // log changes without applying them
	ExcludeUsers     []string // user names or IDs to skip (comma-separated)

	// Debug
	Debug bool
}

// LoadConfig reads configuration from environment variables.
// Required: JELLYFIN_URL, JELLYFIN_API_KEY (or API_KEY for compat)
// Optional: all others with sane defaults.
func LoadConfig() (*Config, error) {
	serverURL := os.Getenv("JELLYFIN_URL")
	apiKey := os.Getenv("JELLYFIN_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("API_KEY") // backward compat
	}

	if serverURL == "" || apiKey == "" {
		return nil, fmt.Errorf("JELLYFIN_URL and JELLYFIN_API_KEY (or API_KEY) are required")
	}

	normalizedURL, err := ValidateServerURL(serverURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	cfg := &Config{
		ServerURL:          normalizedURL,
		APIKey:             apiKey,
		SyncInterval:       getDurationEnv("SYNC_INTERVAL", 20*time.Minute),
		HTTPRequestTimeout: getDurationEnv("HTTP_TIMEOUT", 30*time.Second),
		RetryMaxAttempts:   getIntEnv("RETRY_MAX_ATTEMPTS", 3),
		RetryBaseDelay:     getDurationEnv("RETRY_BASE_DELAY", 2*time.Second),
		CollectionPrefix:   os.Getenv("COLLECTION_PREFIX"),
		DryRun:             getBoolEnv("DRY_RUN", false),
		ExcludeUsers:       parseCommaList(os.Getenv("EXCLUDE_USERS")),
		Debug:              getBoolEnv("DEBUG", false),
	}

	return cfg, nil
}

// IsUserExcluded returns true if the user name or ID is in the exclude list.
func (c *Config) IsUserExcluded(userID, userName string) bool {
	for _, excluded := range c.ExcludeUsers {
		if strings.EqualFold(excluded, userID) || strings.EqualFold(excluded, userName) {
			return true
		}
	}
	return false
}

// PrefixedName returns the collection name with the configured prefix.
func (c *Config) PrefixedName(userName string) string {
	return c.CollectionPrefix + userName
}

// --- env helpers ---

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return d
}

func getIntEnv(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

func getBoolEnv(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return b
}

func parseCommaList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
