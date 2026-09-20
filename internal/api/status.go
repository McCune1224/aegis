package api

import (
	"net/http"
)

// Sizer reports how many rules the running resolver holds. Runtime implements
// it, so the status route can say what actually loaded.
type Sizer interface {
	Size() int
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Upstreams(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	body := map[string]any{"upstreams": names}
	if sizer, ok := s.reloader.(Sizer); ok {
		body["rules"] = sizer.Size()
	}
	writeJSON(w, http.StatusOK, body)
}
