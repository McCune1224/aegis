package main

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/spf13/cobra"

	"aegis/internal/tui"
)

// followPeriod is how often the JSON tail looks for queries it has not printed.
const followPeriod = time.Second

// defaultTailLimit is how much of the log a tail screen holds.
const defaultTailLimit = 200

func newQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Read the recorded query log",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newQueryTailCmd())
	return cmd
}

func newQueryTailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Follow the recorded query log",
		Long: "Show the newest recorded queries and keep the window fresh. " +
			"Keys: q quits. With --json the newest rows are printed once, oldest " +
			"first, and --follow keeps printing the ones that arrive after them.",
		Args: cobra.NoArgs,
		RunE: runQueryTail,
	}

	flags := cmd.Flags()
	flags.String("api-address", defaultAPIAddress, "address of the running aegis HTTP API, as host:port")
	flags.Bool("json", false, "print rows as JSON lines instead of opening a screen")
	flags.Bool("follow", false, "with --json, keep printing rows as they are recorded")
	flags.Int("limit", defaultTailLimit, "how many of the newest queries to read")

	return cmd
}

func runQueryTail(cmd *cobra.Command, _ []string) error {
	cfg, err := loadScreenConfig(cmd)
	if err != nil {
		return err
	}
	baseURL := apiBaseURL(cfg.APIAddress)

	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return err
	}
	if limit <= 0 {
		return errors.New("limit must be a positive number")
	}

	withoutScreen, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	follow, err := cmd.Flags().GetBool("follow")
	if err != nil {
		return err
	}

	switch {
	case withoutScreen && follow:
		return tui.LogFollow(cmd.Context(), cmd.OutOrStdout(), baseURL, limit, followPeriod)
	case withoutScreen:
		return printTail(cmd, baseURL, limit)
	case follow:
		return errors.New("--follow needs --json, the screen already follows")
	default:
		fetch := func() ([]tui.Query, error) { return tui.Log(cmd.Context(), baseURL, limit) }
		return runScreen(cmd.Context(), tui.NewTail(fetch))
	}
}

// printTail writes the newest window once, oldest first, the way tail reads.
func printTail(cmd *cobra.Command, baseURL string, limit int) error {
	rows, err := tui.Log(cmd.Context(), baseURL, limit)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(cmd.OutOrStdout())
	for i := len(rows) - 1; i >= 0; i-- {
		if err := encoder.Encode(rows[i]); err != nil {
			return err
		}
	}
	return nil
}
