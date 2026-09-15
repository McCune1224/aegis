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
				if got := rs.Decide(name, ""); got.Action != filter.ActionAllow {
					b.Fatalf("unexpected action %d", got.Action)
				}
			}
		})
	}
}
