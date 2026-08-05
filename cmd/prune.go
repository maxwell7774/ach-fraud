package cmd

import (
	"context"
	"fmt"

	"github.com/27actions/ach/internal/pipeline"

	"github.com/spf13/cobra"
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Delete artifacts older than --days",
	Long: `Deletes artifact bytes older than the retention window and marks the rows
pruned. Bytes are only removed once no remaining artifact references the same
content checksum, so newer generations of the same file are never touched.

For example:

    ach prune --days 30`,
	RunE: func(cmd *cobra.Command, args []string) error {
		days, err := cmd.Flags().GetInt("days")
		if err != nil {
			return err
		}
		d, _, close, err := open()
		if err != nil {
			return err
		}
		defer close()

		res, err := pipeline.Prune(context.Background(), *d, days)
		if err != nil {
			return err
		}
		fmt.Printf("prune: %d artifacts deleted, %d rows pruned\n", res.Deleted, res.Rows)
		return nil
	},
}

func init() {
	pruneCmd.Flags().Int("days", 0, "delete artifacts older than this many days")
	if err := pruneCmd.MarkFlagRequired("days"); err != nil {
		panic(err)
	}
	rootCmd.AddCommand(pruneCmd)
}
