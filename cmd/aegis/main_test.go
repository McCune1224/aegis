package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionCommandPrintsBuildInfo(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"version"})

	require.NoError(t, cmd.Execute())
	require.Equal(t, "aegis dev (none, unknown)\n", out.String())
}
