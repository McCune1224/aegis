package runtime

import (
	"aegis/internal/safesearch"
	"aegis/internal/store"
)

// safesearchEnables is the stored enablements in the shape the table takes.
func safesearchEnables(specs []store.ProfileSafesearch) []safesearch.Enable {
	enables := make([]safesearch.Enable, 0, len(specs))
	for _, spec := range specs {
		enables = append(enables, safesearch.Enable{Profile: spec.Profile, Engine: spec.Engine})
	}
	return enables
}
