package cmd

import (
	"context"
	"fmt"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var jobsCmd = &cobra.Command{
	Use:   "jobs [state]",
	Short: "List jobs (default: queued)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d, _, close, err := open()
		if err != nil {
			return err
		}
		defer close()

		state := domain.JobQueued
		if len(args) > 0 {
			state = domain.JobState(args[0])
		}
		limit, _ := cmd.Flags().GetInt("limit")

		ctx := context.Background()
		jobs, err := d.Store.ListJobsByState(ctx, state, limit)
		if err != nil {
			return err
		}
		for _, j := range jobs {
			fmt.Printf("%s  %-18s  %s  failures=%d", j.ID, j.Kind, j.Ref, j.Failures)
			if j.LastError != "" {
				fmt.Printf("  last_error=%s", j.LastError)
			}
			fmt.Println()
		}
		return nil
	},
}

var jobsRequeueCmd = &cobra.Command{
	Use:   "requeue <job-id>",
	Short: "Reset a failed job to queued so the next run retries it",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d, _, close, err := open()
		if err != nil {
			return err
		}
		defer close()

		id, err := uuid.Parse(args[0])
		if err != nil {
			return fmt.Errorf("invalid job id %q", args[0])
		}
		if err := d.Store.RequeueJob(context.Background(), id); err != nil {
			return err
		}
		fmt.Printf("requeued %s\n", id)
		return nil
	},
}

func init() {
	jobsCmd.Flags().Int("limit", 100, "maximum number of jobs to list")
	jobsCmd.AddCommand(jobsRequeueCmd)
	rootCmd.AddCommand(jobsCmd)
}
