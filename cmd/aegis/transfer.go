package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"aegis/internal/blocklist"
	"aegis/internal/store"
)

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write the stored configuration as JSON",
		Args:  cobra.NoArgs,
		RunE:  runExport,
	}
	flags := cmd.Flags()
	flags.String("db", "aegis.db", "path to the configuration database")
	flags.StringP("output", "o", "", "write to this file instead of stdout")
	return cmd
}

func runExport(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	flags := cmd.Flags()
	dbPath, _ := flags.GetString("db")

	database, err := store.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	document, err := database.ReadDocument(ctx)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	output, _ := flags.GetString("output")
	if output != "" {
		return os.WriteFile(output, data, 0o644)
	}
	_, err = cmd.OutOrStdout().Write(data)
	return err
}

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import [FILE]",
		Short: "Import a configuration document or a local list file",
		Long: `Import a configuration document or a local list file.

With a JSON document file, every section is upserted into the database: rows the document names are replaced and rows it never mentions are kept, so an import never deletes what the target already had. The document must carry {"version": 1}.

With --source, one local list file is stored as a blocklist source whose url is the file path, so it syncs like any other source.`,
		RunE: runImport,
	}
	flags := cmd.Flags()
	flags.String("db", "aegis.db", "path to the configuration database")
	flags.String("source", "", "store a local list file as a source, as name=path")
	flags.String("format", "hosts", "hosts, domains, or adblock, applied to --source")
	return cmd
}

func runImport(cmd *cobra.Command, args []string) error {
	source, _ := cmd.Flags().GetString("source")
	if source != "" {
		if len(args) != 0 {
			return errors.New("import takes a document file or --source, not both")
		}
		return importSource(cmd, source)
	}
	if len(args) != 1 {
		return errors.New("import needs a document file or --source name=path")
	}
	return importDocument(cmd, args[0])
}

func importDocument(cmd *cobra.Command, path string) error {
	ctx := cmd.Context()
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	document, err := store.ParseDocument(data)
	if err != nil {
		return fmt.Errorf("import %s: %w", path, err)
	}

	dbPath, _ := cmd.Flags().GetString("db")
	database, err := store.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	if err := database.ApplyDocument(ctx, document); err != nil {
		return fmt.Errorf("import %s: %w", path, err)
	}
	return nil
}

func importSource(cmd *cobra.Command, raw string) error {
	ctx := cmd.Context()
	name, path, found := strings.Cut(raw, "=")
	if !found || name == "" || path == "" {
		return fmt.Errorf("source %q must be name=path", raw)
	}
	format, err := blocklist.ParseFormat(cmd.Flag("format").Value.String())
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	dbPath, _ := cmd.Flags().GetString("db")
	database, err := store.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	source := store.Source{Name: name, URL: "file://" + absolute, Format: format, Enabled: true}
	return database.SaveSource(ctx, source)
}
