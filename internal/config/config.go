// Package config loads .achfraudconfig.json, the single source of runtime
// settings for the pipeline.
package config

import (
	"encoding/json"
	"fmt"
	"os"

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
