package cmd

import (
	"context"
	"fmt"

	"github.com/27actions/ach/internal/config"
	"github.com/27actions/ach/internal/importer"
	"github.com/27actions/ach/internal/pgstore"

	"github.com/spf13/cobra"
)

var importHistoryCmd = &cobra.Command{
	Use:   "import-history",
	Short: "Import legacy receiver history from a CSV",
	Long: `Loads approved receiver history from a semicolon-delimited CSV so existing
accounts carry hold history from day one. One submission is created per file,
one batch header per (customer, effective date) group, and one approved hold
per unique (rdfi, account). Rows already in the database are skipped, so
re-running is safe.

CSV format (no header, semicolon-delimited, 6 fields per line):
  <unused>;<receiver name>;<customer id>;<rdfi>;<receiver account>;<effective date YYYYMMDD>

For example:
    ach import-history --file history.csv --amount 100000`,
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath, err := cmd.Flags().GetString("file")
		if err != nil {
			return err
		}
		amount, err := cmd.Flags().GetInt64("amount")
		if err != nil {
			return err
		}
		actor, _ := cmd.Flags().GetString("actor")

		cfg, err := config.Load("")
		if err != nil {
			return err
		}
		if cfg.DBURL == "" {
			return fmt.Errorf("db_url is not configured")
		}

		ctx := context.Background()
		st, err := pgstore.Open(ctx, cfg.DBURL)
		if err != nil {
			return err
		}
		defer st.Close()

		res, err := importer.Import(ctx, st, filePath, amount, actor)
		if err != nil {
			return err
		}

		fmt.Printf("imported %s\n", res.Filename)
		fmt.Printf("  batch headers: %d\n", res.Headers)
		fmt.Printf("  entries: %d\n", res.Entries)
		fmt.Printf("  approved holds: %d\n", res.Holds)
		fmt.Printf("  skipped (already exists): %d\n", res.SkippedExisting)
		fmt.Printf("  skipped (duplicate in file): %d\n", res.SkippedDuplicate)
		return nil
	},
}

func init() {
	importHistoryCmd.Flags().String("file", "", "path to semicolon-delimited CSV (no header)")
	importHistoryCmd.Flags().Int64("amount", 100000, "amount in cents for each entry")
	importHistoryCmd.Flags().String("actor", "importer", "reviewer name recorded on the imported holds")
	if err := importHistoryCmd.MarkFlagRequired("file"); err != nil {
		panic(err)
	}
	rootCmd.AddCommand(importHistoryCmd)
}
