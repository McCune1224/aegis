package filter_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/filter"
)

func BenchmarkDecide(b *testing.B) {
	for _, size := range []int{10, 100_000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			specs := make([]filter.RuleSpec, 0, size)
			for i := range size {
				specs = append(specs, filter.RuleSpec{
					ID:     "r" + strconv.Itoa(i),
					Source: filter.Source{ID: "hagezi", Name: "Hagezi"},
					Kind:   filter.MatchSubdomains,
					Domain: mustParse("blocked" + strconv.Itoa(i) + ".example"),
					Action: filter.ActionBlock,
				})
			}
			rs, err := filter.Compile(defaultConfig(specs))
			require.NoError(b, err)

			name := mustParse("x.ads.doubleclick.net")
			b.ReportAllocs()
			for b.Loop() {
				if got := rs.Decide(name, "", noAddress, testNow); got.Action != filter.ActionAllow {
					b.Fatalf("unexpected action %d", got.Action)
				}
			}
		})
	}
}

// wildcardSpecs builds size wildcard rules whose literal tails are distinct, which
// is the shape a blocklist of wildcards takes.
func wildcardSpecs(size int) []filter.RuleSpec {
	specs := make([]filter.RuleSpec, 0, size)
	for i := range size {
		specs = append(specs, filter.RuleSpec{
			ID:      "r" + strconv.Itoa(i),
			Source:  filter.Source{ID: "hagezi", Name: "Hagezi"},
			Kind:    filter.MatchWildcard,
			Pattern: "*." + strconv.Itoa(i) + ".example",
			Action:  filter.ActionBlock,
		})
	}
	return specs
}

func BenchmarkDecideWildcards(b *testing.B) {
	for _, size := range []int{10, 1000, 10000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			rs, err := filter.Compile(defaultConfig(wildcardSpecs(size)))
			require.NoError(b, err)

			name := mustParse("x.ads.doubleclick.net")
			b.ReportAllocs()
			for b.Loop() {
				if got := rs.Decide(name, "", noAddress, testNow); got.Action != filter.ActionAllow {
					b.Fatalf("unexpected action %d", got.Action)
				}
			}
		})
	}
}

// BenchmarkDecideWildcardsSharingATail is the worst case for indexing on the
// literal tail: every rule ends in the same label and the name ends with it too,
// so no rule can be skipped.
func BenchmarkDecideWildcardsSharingATail(b *testing.B) {
	for _, size := range []int{10, 1000, 10000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			specs := make([]filter.RuleSpec, 0, size)
			for i := range size {
				specs = append(specs, filter.RuleSpec{
					ID:      "r" + strconv.Itoa(i),
					Source:  filter.Source{ID: "hagezi", Name: "Hagezi"},
					Kind:    filter.MatchWildcard,
					Pattern: strconv.Itoa(i) + ".*.example",
					Action:  filter.ActionBlock,
				})
			}
			rs, err := filter.Compile(defaultConfig(specs))
			require.NoError(b, err)

			name := mustParse("x.ads.example")
			b.ReportAllocs()
			for b.Loop() {
				if got := rs.Decide(name, "", noAddress, testNow); got.Action != filter.ActionAllow {
					b.Fatalf("unexpected action %d", got.Action)
				}
			}
		})
	}
}
