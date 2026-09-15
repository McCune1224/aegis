package api

import (
	"os"
	"testing"
)

// Password hashing is deliberately slow. Tests prove the flow, not the cost, so
// they turn the iteration count down for the whole test binary.
func TestMain(m *testing.M) {
	passwordIterations = 1_000
	os.Exit(m.Run())
}
