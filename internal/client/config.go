package client

import "time"

// Config holds configuration options for the HTTP client
type Config struct {
	// Host is the base URL for the GPCN API
	Host string
	// APIKey is the API key for authentication
	APIKey string
	// RequestTimeout is the timeout for individual HTTP requests (default: 60s)
	RequestTimeout time.Duration
	// PollingTimeout is the maximum time to wait for async operations (default: 10m)
	PollingTimeout time.Duration
	// MaxRetries is the maximum number of retries for transient failures (default: 3)
	MaxRetries int
	// InitialRetryDelay is the initial delay before the first retry (default: 1s)
	InitialRetryDelay time.Duration
	// MaxRetryDelay is the maximum delay between retries (default: 30s)
	MaxRetryDelay time.Duration
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig(host, apiKey string) *Config {
	return &Config{
		Host:              host,
		APIKey:            apiKey,
		RequestTimeout:    60 * time.Second,
		PollingTimeout:    10 * time.Minute,
		MaxRetries:        3,
		InitialRetryDelay: 1 * time.Second,
		MaxRetryDelay:     30 * time.Second,
	}
}

// Validate checks that required fields are set
func (c *Config) Validate() error {
	if c.Host == "" {
		return ErrMissingHost
	}
	if c.APIKey == "" {
		return ErrMissingAPIKey
	}
	return nil
}
