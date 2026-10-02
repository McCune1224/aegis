package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

// resetBody is the confirmation the endpoint demands of a wipe.
const resetBody = `{"confirm":"reset"}`

func newResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Wipe clients, discoveries, the query log, services, and settings",
		Long: "Ask a running aegis to wipe what a full reset names: clients, " +
			"discoveries, the query log, the services catalog with every service it " +
			"enabled, and the stored settings with the access lists. Profiles, rules, " +
			"rewrites, schedules, upstreams, sources, and threat feeds stay. " +
			"The server has to be running, and --yes has to be on the command.",
		Args: cobra.NoArgs,
		RunE: runReset,
	}

	flags := cmd.Flags()
	flags.String("api-address", defaultAPIAddress, "address of the running aegis HTTP API, as host:port")
	flags.Bool("yes", false, "confirm the wipe")

	return cmd
}

func runReset(cmd *cobra.Command, _ []string) error {
	confirmed, err := cmd.Flags().GetBool("yes")
	if err != nil {
		return err
	}
	if !confirmed {
		return errors.New("reset: refusing to wipe without --yes")
	}

	cfg, err := loadScreenConfig(cmd)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost,
		apiBaseURL(cfg.APIAddress)+"/api/v1/reset", strings.NewReader(resetBody))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("reset: %s: %s", response.Status, serverMessage(payload))
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.TrimSpace(string(payload)))
	return err
}

// serverMessage reads the reason an API answer carries, so a refused wipe
// prints what the server said instead of only the status line.
func serverMessage(payload []byte) string {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(payload, &body); err == nil && body.Error != "" {
		return body.Error
	}
	return strings.TrimSpace(string(payload))
}
