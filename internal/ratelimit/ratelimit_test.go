package ratelimit_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
	"aegis/internal/ratelimit"
)

func constantKey(key filter.ClientKey) ratelimit.KeyFunc {
	return func(netip.Addr) filter.ClientKey { return key }
}

func TestAllowHoldsABurstAndRefusesTheExcess(t *testing.T) {
	limiter, err := ratelimit.New(ratelimit.Config{Keys: constantKey("phone"), Rate: 1, Burst: 3})
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		require.True(t, limiter.Allow(netip.MustParseAddr("10.0.0.5")), "burst query %d", i)
	}
	for i := 0; i < 2; i++ {
		require.False(t, limiter.Allow(netip.MustParseAddr("10.0.0.5")), "excess query %d", i)
	}
	require.Equal(t, int64(2), limiter.Refused(), "every refused query is counted")
}

func TestAllowKeepsASteadyRateWithinTheLimit(t *testing.T) {
	limiter, err := ratelimit.New(ratelimit.Config{Keys: constantKey("phone"), Rate: 500, Burst: 1})
	require.NoError(t, err)

	address := netip.MustParseAddr("10.0.0.5")
	require.True(t, limiter.Allow(address))
	require.False(t, limiter.Allow(address), "the burst is one query")

	for i := 0; i < 10; i++ {
		time.Sleep(5 * time.Millisecond)
		require.True(t, limiter.Allow(address), "a query every 5ms sits under 500 per second")
	}
	require.Equal(t, int64(1), limiter.Refused(), "only the instant after the drain was refused")
}

func TestAllowKeysTheBucketOnIdentity(t *testing.T) {
	claimed := map[netip.Addr]filter.ClientKey{
		netip.MustParseAddr("10.0.0.1"): "phone",
		netip.MustParseAddr("10.0.0.2"): "laptop",
	}
	limiter, err := ratelimit.New(ratelimit.Config{
		Keys:  func(address netip.Addr) filter.ClientKey { return claimed[address] },
		Rate:  1,
		Burst: 2,
	})
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		require.True(t, limiter.Allow(netip.MustParseAddr("10.0.0.1")))
	}
	require.False(t, limiter.Allow(netip.MustParseAddr("10.0.0.1")), "phone is over its limit")
	require.True(t, limiter.Allow(netip.MustParseAddr("10.0.0.2")), "laptop has its own bucket")
}

func TestAllowSharesOneBucketAcrossOneIdentity(t *testing.T) {
	claimed := map[netip.Addr]filter.ClientKey{
		netip.MustParseAddr("10.0.0.1"): "phone",
		netip.MustParseAddr("10.0.0.9"): "phone",
	}
	limiter, err := ratelimit.New(ratelimit.Config{
		Keys:  func(address netip.Addr) filter.ClientKey { return claimed[address] },
		Rate:  1,
		Burst: 1,
	})
	require.NoError(t, err)

	require.True(t, limiter.Allow(netip.MustParseAddr("10.0.0.1")))
	require.False(t, limiter.Allow(netip.MustParseAddr("10.0.0.9")), "the second address of one device waits on the same bucket")
}

func TestUnclaimedAddressesShareTheDefaultBucket(t *testing.T) {
	limiter, err := ratelimit.New(ratelimit.Config{Keys: constantKey(""), Rate: 1, Burst: 1})
	require.NoError(t, err)

	require.True(t, limiter.Allow(netip.MustParseAddr("192.168.1.50")))
	require.False(t, limiter.Allow(netip.MustParseAddr("192.168.1.51")), "unidentified addresses pool into the default bucket")
}

func TestNewRejectsAnIncompleteConfig(t *testing.T) {
	_, err := ratelimit.New(ratelimit.Config{Rate: 1, Burst: 1})
	require.Error(t, err, "a limiter without identity resolution cannot key a bucket")

	_, err = ratelimit.New(ratelimit.Config{Keys: constantKey("phone"), Burst: 1})
	require.Error(t, err)

	_, err = ratelimit.New(ratelimit.Config{Keys: constantKey("phone"), Rate: 1})
	require.Error(t, err)
}
