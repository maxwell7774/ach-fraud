package cmd

import (
	"context"
	"fmt"

	"github.com/27actions/ach/internal/worker"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Ingests input files and drives the pipeline to completion",
	Long: `Ingests every .ach file in the input directory and drives the job queue
through the pipeline: fix, import, screen, process, publish, archive.

Clean intercept files are published immediately; release files ship only once
their holds are approved. A release waiting on approval is re-checked on every
run. Run is safe to call repeatedly: jobs are idempotent, so work left undone
by a previous run is picked up here.

For example:

    ach run`,
	RunE: func(cmd *cobra.Command, args []string) error {
		d, in, close, err := open()
		if err != nil {
			return err
		}
		defer close()

		ctx := context.Background()
		rep, err := worker.New(*d).Run(ctx, in)
		if err != nil {
			return err
		}
		fmt.Printf("run: ingested %d, skipped %d, completed %d, failed %d, waiting %d",
			rep.Ingested, rep.Skipped, rep.Completed, rep.Failed, rep.Waiting)
		if rep.Recovered > 0 {
			fmt.Printf(", recovered %d", rep.Recovered)
		}
		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}
