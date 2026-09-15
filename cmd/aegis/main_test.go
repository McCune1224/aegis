package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommandErrorSaysAegisOnce(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"serve", "--blocklist", "./does-not-exist.txt"})

	err := root.Execute()
	require.Error(t, err)

	printed := "aegis: " + err.Error()
	require.Equal(t, 1, strings.Count(printed, "aegis:"), "the program name should appear once in %q", printed)
}

func TestABadListFileLeavesNoDatabaseBehind(t *testing.T) {
	database := filepath.Join(t.TempDir(), "aegis.db")
	root := newRootCmd()
	root.SetArgs([]string{"serve", "--db", database, "--blocklist", "./does-not-exist.txt"})

	require.Error(t, root.Execute())
	_, err := os.Stat(database)
	require.ErrorIs(t, err, fs.ErrNotExist)
}
