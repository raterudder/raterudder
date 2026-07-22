package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleListUtilities(w http.ResponseWriter, r *http.Request) {
	utilities := s.utilities.ListUtilities()

	if s.showHidden {
		for i := range utilities {
			utilities[i].Hidden = false
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=300")
	if err := json.NewEncoder(w).Encode(utilities); err != nil {
		panic(http.ErrAbortHandler)
	}
}
