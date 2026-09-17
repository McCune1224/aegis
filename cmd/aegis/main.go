package main

import (
	"fmt"
	"os"

	// Schedules key on local wall-clock time, and release targets such as
	// routers ship no zoneinfo database, so the zone tables ride in the binary.
	_ "time/tzdata"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "aegis",
		Short:         "A DNS sinkhole for focus",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newServeCmd())
	root.AddCommand(newVersionCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the build version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "aegis %s (%s, %s)\n", version, commit, date)
			return err
		},
	}
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "aegis:", err)
		os.Exit(1)
	}
}
