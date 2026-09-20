package main

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"aegis/internal/config"
	"aegis/internal/tui"
)

// defaultAPIAddress is where a serve started with no flags listens, and where
// the operator screens look for it.
const defaultAPIAddress = "127.0.0.1:8080"

// apiBaseURL turns the address a serve listens on into the URL a screen dials.
func apiBaseURL(address string) string { return "http://" + address }

// loadScreenConfig reads just the flags a screen needs. It goes through the
// same loader as serve, so AEGIS_API_ADDRESS means the same thing to both.
func loadScreenConfig(cmd *cobra.Command) (config.Config, error) {
	return config.Load(cmd.Flags())
}

// runScreen hands a model to bubbletea and returns when the operator quits.
func runScreen(ctx context.Context, model tea.Model) error {
	_, err := tea.NewProgram(model, tea.WithContext(ctx)).Run()
	return err
}

func newTopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Watch queries as they happen",
		Long: "Watch the live query stream from a running aegis. Keys: q quits, " +
			"space stops the screen so a row can be read, and space again resumes it.",
		Args: cobra.NoArgs,
		RunE: runTop,
	}

	flags := cmd.Flags()
	flags.String("api-address", defaultAPIAddress, "address of the running aegis HTTP API, as host:port")
	flags.Bool("json", false, "print one JSON object per decision instead of opening a screen")

	return cmd
}

func runTop(cmd *cobra.Command, _ []string) error {
	cfg, err := loadScreenConfig(cmd)
	if err != nil {
		return err
	}
	baseURL := apiBaseURL(cfg.APIAddress)

	if withoutScreen, _ := cmd.Flags().GetBool("json"); withoutScreen {
		return tui.StreamJSON(cmd.Context(), cmd.OutOrStdout(), baseURL)
	}

	events, err := tui.Stream(cmd.Context(), baseURL)
	if err != nil {
		return err
	}
	return runScreen(cmd.Context(), tui.NewTop(events))
}
