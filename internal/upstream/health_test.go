package upstream

// The health math is pure, so its exact values are pinned here: the smoothing
// divisor, the failure threshold, and the doubling backoff are the numbers an
// operator's failover behaviour actually follows.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBackoffForDoublesFromTheBaseAndCaps(t *testing.T) {
	require.Equal(t, 10*time.Second, backoffFor(failureThreshold))
	require.Equal(t, 20*time.Second, backoffFor(failureThreshold+1))
	require.Equal(t, 40*time.Second, backoffFor(failureThreshold+2))
	require.Equal(t, 10*time.Minute, backoffFor(failureThreshold+30), "the doubling caps at the maximum")
}

func TestRecordFoldsASampleIntoTheEWMA(t *testing.T) {
	pool := &Pool{now: func() time.Time {
		return time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	}}
	peer := &peer{ewma: 80 * time.Millisecond}
	pool.record(peer, 120*time.Millisecond, nil)
	require.Equal(t, 90*time.Millisecond, peer.ewma, "ewma moves a quarter of the way toward the sample")
}

func TestRecordKeepsTheFailureCountAndSchedulesTheBackoff(t *testing.T) {
	pool := &Pool{now: func() time.Time {
		return time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	}}
	peer := &peer{}

	pool.record(peer, 0, errTestDown{})
	require.Equal(t, 1, peer.failures)
	require.True(t, peer.downUntil.IsZero(), "one failure alone does not mark the resolver down")

	pool.record(peer, 0, errTestDown{})
	require.Equal(t, failureThreshold, peer.failures)
	require.Equal(t, pool.now().Add(10*time.Second), peer.downUntil)

	pool.record(peer, 20*time.Millisecond, nil)
	require.Equal(t, 0, peer.failures, "a success clears the failure count")
	require.True(t, peer.downUntil.IsZero(), "a success clears the backoff")
	require.Equal(t, 20*time.Millisecond, peer.ewma, "the first sample seeds the average")
}

type errTestDown struct{}

func (errTestDown) Error() string { return "resolver down" }
