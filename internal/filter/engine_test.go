package filter_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
)

func TestEngineAllowsBeforeAnythingIsPublished(t *testing.T) {
	eng := filter.New()

	got := eng.Decide(mustParse("example.com"))

	require.Equal(t, filter.ActionAllow, got.Action)
	require.Nil(t, got.Match)
}

func TestEngineDecideUsesTheLatestPublishedSet(t *testing.T) {
	eng := filter.New()

	eng.Publish(compile(t, hagezi("block-ads", filter.MatchSubdomains, "doubleclick.net")))
	require.Equal(t, filter.ActionBlock, eng.Decide(mustParse("ads.doubleclick.net")).Action)

	eng.Publish(compile(t))
	require.Equal(t, filter.ActionAllow, eng.Decide(mustParse("ads.doubleclick.net")).Action)
}

func TestEngineSnapshotAnswersWithTheSetItWasTakenFrom(t *testing.T) {
	eng := filter.New()
	eng.Publish(compile(t, hagezi("block-ads", filter.MatchSubdomains, "doubleclick.net")))

	snapshot := eng.Snapshot()
	eng.Publish(compile(t))

	require.Equal(t, filter.ActionBlock, snapshot.Decide(mustParse("ads.doubleclick.net")).Action)
	require.Equal(t, filter.ActionAllow, eng.Decide(mustParse("ads.doubleclick.net")).Action)
}

func TestEngineRefusesToPublishNil(t *testing.T) {
	eng := filter.New()

	require.Panics(t, func() { eng.Publish(nil) })
}

func TestEngineSwapIsAtomicUnderLoad(t *testing.T) {
	blocking := compile(t, hagezi("block-ads", filter.MatchSubdomains, "doubleclick.net"))
	allowing := compile(t)

	eng := filter.New()
	eng.Publish(blocking)

	name := mustParse("ads.doubleclick.net")
	stop := make(chan struct{})
	torn := make(chan string, 8)

	var readers sync.WaitGroup
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				got := eng.Decide(name)
				switch {
				case got.Action == filter.ActionAllow && got.Match == nil:
				case got.Action == filter.ActionBlock && got.Match != nil && got.Match.RuleID == "block-ads":
				default:
					torn <- fmt.Sprintf("action=%d match=%v", got.Action, got.Match)
					return
				}
			}
		}()
	}

	for round := range 2000 {
		if round%2 == 0 {
			eng.Publish(allowing)
			continue
		}
		eng.Publish(blocking)
	}

	close(stop)
	readers.Wait()
	close(torn)

	for msg := range torn {
		t.Errorf("verdict was not from one rule set: %s", msg)
	}
}
