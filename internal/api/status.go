package api

import "net/http"

// Sizer reports how many rules the running resolver holds. Runtime implements
// it, so the status route can say what actually loaded.
type Sizer interface {
	Size() int
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{"upstream": s.upstream}
	if sizer, ok := s.reloader.(Sizer); ok {
		body["rules"] = sizer.Size()
	}
	writeJSON(w, http.StatusOK, body)
}
