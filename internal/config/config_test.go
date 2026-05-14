package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paper-claw/internal/config"
)

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "paperclaw.yaml")
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func setSecrets(t *testing.T) {
	t.Helper()
	t.Setenv("OAUTH_CLIENT_SECRET", "test-secret")
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
}

const minimalYAML = `
http:
  bind_addr: ":9090"
  public_base_url: "https://example.com"
drive:
  inbox_folder_id: "inbox-id"
  library_folder_id: "lib-id"
  processed_folder_id: "proc-id"
oauth:
  client_id: "client-id"
  redirect_path: "/oauth/callback"
state:
  dir: "/tmp/state"
`

func TestLoad_Happy(t *testing.T) {
	setSecrets(t)
	path := writeYAML(t, minimalYAML)

	cfg, secrets, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.HTTP.BindAddr != ":9090" {
		t.Errorf("bind_addr = %q; want %q", cfg.HTTP.BindAddr, ":9090")
	}
	if cfg.Drive.InboxFolderID != "inbox-id" {
		t.Errorf("inbox_folder_id = %q; want %q", cfg.Drive.InboxFolderID, "inbox-id")
	}
	if secrets.OAuthClientSecret != "test-secret" {
		t.Error("OAuthClientSecret not loaded from env")
	}
	if secrets.AnthropicAPIKey != "test-key" {
		t.Error("AnthropicAPIKey not loaded from env")
	}
}

func TestLoad_Defaults(t *testing.T) {
	setSecrets(t)
	// Only required fields; leave knobs at zero to trigger defaults.
	const y = `
http:
  public_base_url: "https://example.com"
drive:
  inbox_folder_id: "i"
  library_folder_id: "l"
  processed_folder_id: "p"
oauth:
  client_id: "c"
state:
  dir: "/tmp/s"
`
	cfg, _, err := config.Load(writeYAML(t, y))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.HTTP.BindAddr != ":8080" {
		t.Errorf("default BindAddr = %q; want :8080", cfg.HTTP.BindAddr)
	}
	if cfg.Poll.Interval != 5*time.Minute {
		t.Errorf("default Poll.Interval = %v; want 5m", cfg.Poll.Interval)
	}
	if cfg.Poll.StableThreshold != 30*time.Second {
		t.Errorf("default Poll.StableThreshold = %v; want 30s", cfg.Poll.StableThreshold)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("default Log.Level = %q; want info", cfg.Log.Level)
	}
	if cfg.OAuth.RedirectPath != "/oauth/callback" {
		t.Errorf("default RedirectPath = %q; want /oauth/callback", cfg.OAuth.RedirectPath)
	}
	if cfg.Webhook.ChannelTTL != 7*24*time.Hour {
		t.Errorf("default ChannelTTL = %v; want 168h", cfg.Webhook.ChannelTTL)
	}
	if cfg.Webhook.RenewLeadTime != time.Hour {
		t.Errorf("default RenewLeadTime = %v; want 1h", cfg.Webhook.RenewLeadTime)
	}
}

func TestLoad_MissingRequiredFields(t *testing.T) {
	setSecrets(t)
	// Completely empty config — all required fields missing.
	path := writeYAML(t, "{}")
	_, _, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for empty config")
	}

	for _, field := range []string{
		"http.public_base_url",
		"drive.inbox_folder_id",
		"drive.library_folder_id",
		"drive.processed_folder_id",
		"oauth.client_id",
		"state.dir",
	} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error should mention %q; got: %v", field, err)
		}
	}
}

func TestLoad_MissingSecrets(t *testing.T) {
	t.Setenv("OAUTH_CLIENT_SECRET", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	path := writeYAML(t, minimalYAML)
	_, _, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for missing secrets")
	}
	if !strings.Contains(err.Error(), "OAUTH_CLIENT_SECRET") {
		t.Errorf("error should mention OAUTH_CLIENT_SECRET; got: %v", err)
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("error should mention ANTHROPIC_API_KEY; got: %v", err)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, _, err := config.Load("/nonexistent/path/paperclaw.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeYAML(t, "this: is: not: valid: yaml: !!!")
	_, _, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoad_CustomPollInterval(t *testing.T) {
	setSecrets(t)
	const y = `
http:
  public_base_url: "https://example.com"
drive:
  inbox_folder_id: "i"
  library_folder_id: "l"
  processed_folder_id: "p"
oauth:
  client_id: "c"
state:
  dir: "/tmp/s"
poll:
  interval: 10m
  stable_threshold: 45s
`
	cfg, _, err := config.Load(writeYAML(t, y))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Poll.Interval != 10*time.Minute {
		t.Errorf("Poll.Interval = %v; want 10m", cfg.Poll.Interval)
	}
	if cfg.Poll.StableThreshold != 45*time.Second {
		t.Errorf("Poll.StableThreshold = %v; want 45s", cfg.Poll.StableThreshold)
	}
}
