// Package config loads .achfraudconfig.json, the single source of runtime
// settings for the pipeline.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/27actions/ach/internal/domain"
)

const DefaultPath = ".achfraudconfig.json"

type Config struct {
	DBURL            string `json:"db_url"`
	InputDir         string `json:"input_dir"`
	ArtifactStoreDir string `json:"artifact_store_dir"`
	OutgoingDir      string `json:"outgoing_dir"`
	HTTPAddr         string `json:"http_addr"`
	HoldDays         int    `json:"hold_days"`
	HoldingAccount   string `json:"holding_account"`
	HoldingRDFI      string `json:"holding_rdfi"`
	HoldSingleAmount int64  `json:"hold_single_amount"`
	HoldVelocityAmt  int64  `json:"hold_velocity_amount"`
	DedupWindowDays  int    `json:"dedup_window_days"`

	// Entra (OAuth2/OIDC) authentication. When AuthDisabled is true the web API
	// accepts the X-Actor header instead of requiring a login (local dev/e2e).
	EntraTenantID     string `json:"entra_tenant_id"`
	EntraClientID     string `json:"entra_client_id"`
	EntraClientSecret string `json:"entra_client_secret"`
	EntraRedirectURL  string `json:"entra_redirect_url"`
	SessionTTLHours   int    `json:"session_ttl_hours"`
	// CookieSecure defaults to true (Secure session cookie). It is a pointer so
	// "absent" means secure; only an explicit false opts into a plain-HTTP
	// cookie for local dev.
	CookieSecure *bool `json:"cookie_secure"`
	AuthDisabled bool  `json:"auth_disabled"`

	// Graph mail: app-only (client-credentials) emails from a shared mailbox,
	// using the same Entra app registration as login (Mail.Send app permission).
	MailEnabled   bool   `json:"mail_enabled"`
	SharedMailbox string `json:"shared_mailbox"`
	AlertEmails   string `json:"alert_emails"`
	AppBaseURL    string `json:"app_base_url"`
}

// EntraEnabled reports whether OIDC auth is configured and enabled.
func (c *Config) EntraEnabled() bool {
	return !c.AuthDisabled && c.EntraTenantID != "" && c.EntraClientID != ""
}

// CookieSecureEnabled reports whether the session cookie should carry the
// Secure flag. It defaults to true; only an explicit "cookie_secure": false
// opts into a plain-HTTP cookie (local dev).
func (c *Config) CookieSecureEnabled() bool {
	return c.CookieSecure == nil || *c.CookieSecure
}

// SessionTTL returns the session lifetime, defaulting to 8 hours.
func (c *Config) SessionTTL() time.Duration {
	if c.SessionTTLHours <= 0 {
		return 8 * time.Hour
	}
	return time.Duration(c.SessionTTLHours) * time.Hour
}

// MailConfigured reports whether Graph mail alerts are configured and enabled.
// It reuses the Entra app registration (tenant + client id/secret) for the
// client-credentials flow, so only the mailbox and recipients need separate
// settings.
func (c *Config) MailConfigured() bool {
	return c.MailEnabled &&
		c.EntraTenantID != "" && c.EntraClientID != "" && c.EntraClientSecret != "" &&
		c.SharedMailbox != "" && c.AlertEmails != ""
}

// AlertEmailsList splits the comma-separated recipient list.
func (c *Config) AlertEmailsList() []string {
	var out []string
	for _, e := range strings.Split(c.AlertEmails, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// Policy returns the hold rules as domain policy, applying the config
// defaults for any value left at zero.
func (c *Config) Policy() domain.Policy {
	p := domain.Policy{
		HoldingRDFI:     c.HoldingRDFI,
		HoldingAccount:  c.HoldingAccount,
		DedupWindowDays: c.DedupWindowDays,
	}
	if c.HoldDays <= 0 {
		p.HoldDays = 30
	} else {
		p.HoldDays = c.HoldDays
	}
	if c.DedupWindowDays <= 0 {
		p.DedupWindowDays = 365
	}
	if c.HoldSingleAmount <= 0 {
		p.HoldSingleAmount = 100000
	} else {
		p.HoldSingleAmount = c.HoldSingleAmount
	}
	if c.HoldVelocityAmt <= 0 {
		p.HoldVelocityAmount = 100000
	} else {
		p.HoldVelocityAmount = c.HoldVelocityAmt
	}
	return p
}

// Validate fails fast on a misconfigured pipeline before any jobs run, so a
// config typo surfaces at the CLI instead of as a failed process job.
func (c *Config) Validate() error {
	if len(c.HoldingRDFI) != 9 {
		return fmt.Errorf("holding_rdfi must be 9 digits, got %q", c.HoldingRDFI)
	}
	if c.HoldingAccount == "" {
		return fmt.Errorf("holding_account is not configured")
	}
	return nil
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return &cfg, nil
}
