// Package cmd is the thin CLI over the pipeline. Commands load config, build
// the adapters, call pipeline/worker functions, and print results. No business
// logic lives here.
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/27actions/ach/internal/clock"
	"github.com/27actions/ach/internal/config"
	"github.com/27actions/ach/internal/graphmail"
	"github.com/27actions/ach/internal/localfiles"
	"github.com/27actions/ach/internal/localinput"
	"github.com/27actions/ach/internal/notifier"
	"github.com/27actions/ach/internal/pgstore"
	"github.com/27actions/ach/internal/pipeline"
	"github.com/27actions/ach/internal/ports"
	"github.com/27actions/ach/internal/sender"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ach",
	Short: "ACH fraud pre-processor",
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// open builds the pipeline dependencies and the intake adapter from config.
// The returned closer releases the database pool.
func open() (*pipeline.Deps, ports.Input, func(), error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, nil, nil, err
	}
	if cfg.ArtifactStoreDir == "" {
		return nil, nil, nil, fmt.Errorf("artifact_store_dir is not configured")
	}
	for _, dir := range []string{cfg.InputDir, cfg.ArtifactStoreDir, cfg.OutgoingDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, nil, fmt.Errorf("creating dir %s: %w", dir, err)
		}
	}
	ctx := context.Background()
	st, err := pgstore.Open(ctx, cfg.DBURL)
	if err != nil {
		return nil, nil, nil, err
	}
	d := &pipeline.Deps{
		Store:    st,
		Files:    localfiles.New(cfg.ArtifactStoreDir),
		Sender:   sender.New(cfg.OutgoingDir),
		Notifier: buildNotifier(st, cfg),
		Clock:    clock.Real{},
		Policy:   cfg.Policy(),
	}
	return d, localinput.New(cfg.InputDir), st.Close, nil
}

// buildNotifier returns the Graph-backed email notifier when configured,
// otherwise the no-op.
func buildNotifier(st *pgstore.Store, cfg *config.Config) ports.Notifier {
	if !cfg.MailConfigured() {
		return notifier.Noop{}
	}
	mc := graphmail.New(graphmail.Config{
		TenantID:     cfg.GraphTenantID,
		ClientID:     cfg.GraphClientID,
		ClientSecret: cfg.GraphClientSecret,
	})
	return notifier.NewGraph(st, mc, cfg.SharedMailbox, cfg.AlertEmailsList(), cfg.AppBaseURL)
}
