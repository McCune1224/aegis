package runtime_test

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/runtime"
	"aegis/internal/services"
)

// TestAReloadReportsWhatItPublished is the deploy-time signal for a
// configuration write: the log names how big the rule set that went live is
// and how much of it came from enabled services, so an operator can watch a
// saved service reach the filter without opening the database.
func TestAReloadReportsWhatItPublished(t *testing.T) {
	ctx := t.Context()
	s := openStore(t)
	require.NoError(t, s.SaveCatalog(ctx, time.Unix(1_700_000_100, 0), []services.Service{
		{ID: "youtube", Name: "YouTube", Group: "streaming", Rules: []string{"||youtube.com^", "||youtu.be^"}},
	}))

	var logs bytes.Buffer
	rt := runtime.New(s, nil, slog.New(slog.NewTextHandler(&logs, nil)))
	require.NoError(t, rt.Reload(ctx))
	require.Contains(t, logs.String(), "config published", logs.String())
	require.Contains(t, logs.String(), "service_rules=0", "nothing is enabled yet", logs.String())

	logs.Reset()
	require.NoError(t, s.SetProfileServices(ctx, "default", []string{"youtube"}))
	require.NoError(t, rt.Reload(ctx))
	require.Contains(t, logs.String(), "config published", logs.String())
	require.Contains(t, logs.String(), "service_rules=2", "the save turned both catalog rules live", logs.String())
}
