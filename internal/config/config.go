// Package config loads and validates paperclaw daemon configuration.
// Daemon knobs come from a YAML file; secrets come from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all daemon knobs read from the YAML config file.
type Config struct {
	HTTP    HTTPConfig    `yaml:"http"`
	Drive   DriveConfig   `yaml:"drive"`
	OAuth   OAuthConfig   `yaml:"oauth"`
	Poll    PollConfig    `yaml:"poll"`
	State   StateConfig   `yaml:"state"`
	Log     LogConfig     `yaml:"log"`
	Webhook WebhookConfig `yaml:"webhook"`
}

// HTTPConfig controls the HTTP server.
type HTTPConfig struct {
	BindAddr      string `yaml:"bind_addr"`
	PublicBaseURL string `yaml:"public_base_url"`
}

// DriveConfig identifies the Google Drive folders by ID.
type DriveConfig struct {
	InboxFolderID     string `yaml:"inbox_folder_id"`
	LibraryFolderID   string `yaml:"library_folder_id"`
	ProcessedFolderID string `yaml:"processed_folder_id"`
}

// OAuthConfig holds OAuth application settings.
type OAuthConfig struct {
	ClientID     string `yaml:"client_id"`
	RedirectPath string `yaml:"redirect_path"`
}

// PollConfig controls the inbox polling schedule.
type PollConfig struct {
	Interval        time.Duration `yaml:"interval"`
	StableThreshold time.Duration `yaml:"stable_threshold"`
}

// StateConfig points to the directory that holds paperclaw.db and other state.
type StateConfig struct {
	Dir string `yaml:"dir"`
}

// LogConfig controls structured logging.
type LogConfig struct {
	Level string `yaml:"level"`
}

// WebhookConfig controls Drive push-notification channels (used in Phase 3).
type WebhookConfig struct {
	ChannelTTL    time.Duration `yaml:"channel_ttl"`
	RenewLeadTime time.Duration `yaml:"renew_lead_time"`
}

// Secrets holds sensitive values sourced exclusively from environment variables.
type Secrets struct {
	OAuthClientSecret string
	AnthropicAPIKey   string
}

// Load reads the YAML file at path, applies defaults, reads env secrets, and
// validates all required fields. It returns an aggregated error listing every
// missing or invalid field so callers can fix all problems in one pass.
func Load(path string) (*Config, *Secrets, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, nil, fmt.Errorf("parsing config: %w", err)
	}

	applyDefaults(&cfg)

	// Fields with defaults (BindAddr, RedirectPath, Poll.*, Log.Level, Webhook.*)
	// are populated by applyDefaults before this point.
	var missing []string

	if cfg.HTTP.PublicBaseURL == "" {
		missing = append(missing, "http.public_base_url")
	}
	if cfg.Drive.InboxFolderID == "" {
		missing = append(missing, "drive.inbox_folder_id")
	}
	if cfg.Drive.LibraryFolderID == "" {
		missing = append(missing, "drive.library_folder_id")
	}
	if cfg.Drive.ProcessedFolderID == "" {
		missing = append(missing, "drive.processed_folder_id")
	}
	if cfg.OAuth.ClientID == "" {
		missing = append(missing, "oauth.client_id")
	}
	if cfg.State.Dir == "" {
		missing = append(missing, "state.dir")
	}

	secrets, secretErrs := loadSecrets()
	missing = append(missing, secretErrs...)

	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("config validation errors:\n  %s",
			strings.Join(missing, "\n  "))
	}

	return &cfg, secrets, nil
}

func applyDefaults(cfg *Config) {
	if cfg.HTTP.BindAddr == "" {
		cfg.HTTP.BindAddr = ":8080"
	}
	if cfg.Poll.Interval == 0 {
		cfg.Poll.Interval = 5 * time.Minute
	}
	if cfg.Poll.StableThreshold == 0 {
		cfg.Poll.StableThreshold = 30 * time.Second
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Webhook.ChannelTTL == 0 {
		cfg.Webhook.ChannelTTL = 7 * 24 * time.Hour
	}
	if cfg.Webhook.RenewLeadTime == 0 {
		cfg.Webhook.RenewLeadTime = 1 * time.Hour
	}
	if cfg.OAuth.RedirectPath == "" {
		cfg.OAuth.RedirectPath = "/oauth/callback"
	}
}

func loadSecrets() (*Secrets, []string) {
	var s Secrets
	var missing []string

	s.OAuthClientSecret = os.Getenv("OAUTH_CLIENT_SECRET")
	if s.OAuthClientSecret == "" {
		missing = append(missing, "env OAUTH_CLIENT_SECRET is required")
	}

	s.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	if s.AnthropicAPIKey == "" {
		missing = append(missing, "env ANTHROPIC_API_KEY is required")
	}

	return &s, missing
}
