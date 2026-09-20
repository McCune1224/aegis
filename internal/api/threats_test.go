package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/store"
)

func TestListThreatFindingsServesRecordedFindings(t *testing.T) {
	h := startHarness(t)
	when := time.Unix(1_700_000_100, 0).UTC()

	batch := []store.ThreatFinding{
		{
			Time:     when,
			Client:   "phone",
			Kind:     "dga",
			Summary:  "asked for 16 generated-looking domains in 10m0s",
			Evidence: []string{"xkvzwmqpoierutti.biz"},
		},
	}
	require.NoError(t, h.database.RecordThreatFindings(t.Context(), batch))

	status, body := h.do(t, http.MethodGet, "/api/v1/threats/findings", "")

	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"kind":"dga"`)
	require.Contains(t, body, `"client":"phone"`)
	require.Contains(t, body, `"summary":"asked for 16 generated-looking domains in 10m0s"`)
	require.Contains(t, body, "xkvzwmqpoierutti.biz")
}

func TestListThreatFindingsRejectsABadLimit(t *testing.T) {
	h := startHarness(t)

	status, body := h.do(t, http.MethodGet, "/api/v1/threats/findings?limit=zero", "")

	require.Equal(t, http.StatusBadRequest, status, body)
}
