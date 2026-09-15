package main

import (
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
