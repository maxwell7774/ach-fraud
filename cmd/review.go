package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/27actions/ach/internal/pipeline"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review holds",
}

var reviewListCmd = &cobra.Command{
	Use:   "list [status]",
	Short: "List holds (optionally filtered by status)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d, _, close, err := open()
		if err != nil {
			return err
		}
		defer close()

		filter := ""
		if len(args) > 0 {
			filter = args[0]
		}
		ctx := context.Background()
		holds, err := d.Store.ListAllHolds(ctx)
		if err != nil {
			return err
		}
		for _, h := range holds {
			if filter != "" && string(h.Status) != filter {
				continue
			}
			fmt.Printf("%s  %-14s  %-9s  %-17s  $%9.2f  %s\n",
				h.ID, h.Status, h.EntryRdfi, h.EntryReceiverAcct, float64(h.EntryAmount)/100, h.EntryTrace)
		}
		return nil
	},
}

var reviewApproveCmd = &cobra.Command{
	Use:   "approve <hold-id>",
	Short: "Approve a pending hold and release the funds",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d, _, close, err := open()
		if err != nil {
			return err
		}
		defer close()

		id, err := uuid.Parse(args[0])
		if err != nil {
			return fmt.Errorf("invalid hold id %q", args[0])
		}
		actor, _ := cmd.Flags().GetString("actor")
		note, _ := cmd.Flags().GetString("note")
		if err := pipeline.ApproveHold(context.Background(), *d, id, actor, note); err != nil {
			if errors.Is(err, pipeline.ErrAlreadyReviewed) {
				fmt.Printf("already approved %s\n", id)
				return nil
			}
			return err
		}
		fmt.Printf("approved %s\n", id)
		return nil
	},
}

var reviewDeclineCmd = &cobra.Command{
	Use:   "decline <hold-id>",
	Short: "Decline a pending hold",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d, _, close, err := open()
		if err != nil {
			return err
		}
		defer close()

		id, err := uuid.Parse(args[0])
		if err != nil {
			return fmt.Errorf("invalid hold id %q", args[0])
		}
		actor, _ := cmd.Flags().GetString("actor")
		note, _ := cmd.Flags().GetString("note")
		if err := pipeline.DeclineHold(context.Background(), *d, id, actor, note); err != nil {
			if errors.Is(err, pipeline.ErrAlreadyReviewed) {
				fmt.Printf("already declined %s\n", id)
				return nil
			}
			return err
		}
		fmt.Printf("declined %s\n", id)
		return nil
	},
}

func init() {
	reviewCmd.AddCommand(reviewListCmd)
	reviewCmd.AddCommand(reviewApproveCmd)
	reviewCmd.AddCommand(reviewDeclineCmd)

	reviewApproveCmd.Flags().String("actor", "cli", "reviewer name")
	reviewApproveCmd.Flags().String("note", "", "review note")
	reviewDeclineCmd.Flags().String("actor", "cli", "reviewer name")
	reviewDeclineCmd.Flags().String("note", "", "review note")

	rootCmd.AddCommand(reviewCmd)
}
