package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleListESS(w http.ResponseWriter, r *http.Request) {
	systems := s.ess.ListSystems(r.Context())

	if s.showHidden {
		for i := range systems {
			systems[i].Hidden = false
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(systems); err != nil {
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
}
