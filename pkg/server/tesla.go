package server

import (
	"net/http"
)

func (s *Server) handleTeslaRegister(w http.ResponseWriter, r *http.Request) {
	user := s.getUser(r)
	if !s.isMultiSiteAdmin(user) {
		writeJSONError(w, "unauthorized", http.StatusForbidden)
		return
	}

	domain := r.Host
	if domain == "" {
		writeJSONError(w, "domain not found in http request", http.StatusBadRequest)
		return
	}

	if err := s.ess.RegisterTesla(r.Context(), domain); err != nil {
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleTeslaPublicKey(w http.ResponseWriter, r *http.Request) {
	pubKeyPEM := s.ess.TeslaPublicKeyPEM()
	if pubKeyPEM == "" {
		http.Error(w, "not configured", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	if _, err := w.Write([]byte(pubKeyPEM)); err != nil {
		panic(http.ErrAbortHandler)
	}
}
