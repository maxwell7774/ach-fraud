package cmd

import (
	"context"
	"fmt"
	"log"

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

At the end of the run one digest email is sent (when email is configured and
something alert-worthy happened): the pending holds, velocity leaks, blocked
releases, and failures from this run.

For example:

    ach run`,
	RunE: func(cmd *cobra.Command, args []string) error {
		d, in, close, err := open()
		if err != nil {
			return err
		}
		defer close()

		ctx := context.Background()
		// Flush the run digest even if the run errored part-way, so the events
		// that did occur are still reported.
		defer func() {
			if ferr := d.Notifier.Flush(ctx); ferr != nil {
				log.Printf("run: email digest: %v", ferr)
			}
		}()

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
