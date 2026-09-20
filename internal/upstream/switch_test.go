package upstream_test

import (
	"context"
	"testing"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/require"

	"aegis/internal/upstream"
)

func TestASwitchWithoutAPoolNamesTheProblem(t *testing.T) {
	sw := upstream.NewSwitch()

	_, err := sw.Resolve(context.Background(), new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA))

	require.EqualError(t, err, "upstream: no resolvers are loaded")
}

func TestASwitchServesThePoolItWasGivenLast(t *testing.T) {
	first := startStub(t, answerWith("203.0.113.10"))
	second := startStub(t, answerWith("203.0.113.11"))
	sw := upstream.NewSwitch()
	pool, err := upstream.New(upstream.Config{Specs: specs(t, first.address)})
	require.NoError(t, err)
	sw.Swap(pool)
	require.Equal(t, "203.0.113.10", whoAnswered(t, resolveThrough(t, sw)))

	pool, err = upstream.New(upstream.Config{Specs: specs(t, second.address)})
	require.NoError(t, err)
	sw.Swap(pool)

	require.Equal(t, "203.0.113.11", whoAnswered(t, resolveThrough(t, sw)), "the same handle answers through the new pool")
}

func specs(t *testing.T, raw ...string) []upstream.Spec {
	t.Helper()
	parsed := make([]upstream.Spec, 0, len(raw))
	for _, entry := range raw {
		spec, err := upstream.Parse(entry)
		require.NoError(t, err)
		parsed = append(parsed, spec)
	}
	return parsed
}

func resolveThrough(t *testing.T, sw *upstream.Switch) *mdns.Msg {
	t.Helper()
	req := new(mdns.Msg).SetQuestion("example.com.", mdns.TypeA)
	resp, err := sw.Resolve(context.Background(), req)
	require.NoError(t, err)
	return resp
}
