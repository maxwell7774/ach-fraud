package cmd

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/27actions/ach/internal/config"
	"github.com/27actions/ach/internal/migrate"

	"github.com/pressly/goose/v3"
	"github.com/spf13/cobra"
	// Registers the "pgx" driver for database/sql.
	_ "github.com/jackc/pgx/v5/stdlib"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply database migrations",
	Long: `Applies the goose schema migrations embedded in the binary against the
configured database (db_url). Idempotent: re-running applies only what is
pending. For example:

    ach migrate`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load("")
		if err != nil {
			return err
		}
		if cfg.DBURL == "" {
			return fmt.Errorf("db_url is not configured")
		}
		ctx := context.Background()
		db, err := sql.Open("pgx", cfg.DBURL)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := db.PingContext(ctx); err != nil {
			return fmt.Errorf("connecting to database: %w", err)
		}

		goose.SetBaseFS(migrate.FS)
		goose.SetDialect("postgres")
		if err := goose.UpContext(ctx, db, "schema"); err != nil {
			return fmt.Errorf("applying migrations: %w", err)
		}
		fmt.Println("migrations applied")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}
