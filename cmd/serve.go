package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/27actions/ach/internal/auth"
	"github.com/27actions/ach/internal/config"
	"github.com/27actions/ach/internal/httpapi"

	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the review web UI",
	Long: `Serves the review web UI: the JSON API under /api and the built
frontend from web/dist. Approve/decline actions go through the same audit
trail as the review CLI.

For example:

    ach serve`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load("")
		if err != nil {
			return err
		}
		addr := cfg.HTTPAddr
		if flag, _ := cmd.Flags().GetString("addr"); flag != "" {
			addr = flag
		}
		if addr == "" {
			addr = ":8080"
		}

		d, _, close, err := open()
		if err != nil {
			return err
		}
		defer close()

		srv := httpapi.New(*d)
		if cfg.EntraEnabled() {
			ent, err := auth.NewProvider(auth.Config{
				TenantID:     cfg.EntraTenantID,
				ClientID:     cfg.EntraClientID,
				ClientSecret: cfg.EntraClientSecret,
				RedirectURL:  cfg.EntraRedirectURL,
			})
			if err != nil {
				return fmt.Errorf("entra auth: %w", err)
			}
			srv.EnableAuth(ent, &auth.SessionManager{
				Store: d.Store,
				TTL:   cfg.SessionTTL(),
			}, cfg.CookieSecureEnabled())
		}

		server := &http.Server{
			Addr:    addr,
			Handler: srv.Handler(),
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		errCh := make(chan error, 1)
		go func() {
			fmt.Printf("serving on %s\n", addr)
			errCh <- server.ListenAndServe()
		}()

		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
		case <-ctx.Done():
			fmt.Println("\nshutting down")
			return server.Shutdown(context.Background())
		}
		return nil
	},
}

func init() {
	serveCmd.Flags().String("addr", "", "listen address (overrides config http_addr)")
	rootCmd.AddCommand(serveCmd)
}
