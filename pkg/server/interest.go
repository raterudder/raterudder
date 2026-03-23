package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
)

func (s *Server) handleSubmitInterest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Utility             string `json:"utility"`
		Battery             string `json:"battery"`
		UtilityProviderName string `json:"utilityProviderName"`
		State               string `json:"state"`
		PlanName            string `json:"planName"`
		BatteryName         string `json:"batteryName"`
		Comments            string `json:"comments"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to decode interest body", slog.Any("error", err))
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Utility == "" && req.Battery == "" && req.UtilityProviderName == "" && req.State == "" && req.PlanName == "" && req.BatteryName == "" && req.Comments == "" {
		writeJSONError(w, "at least one field must be provided", http.StatusBadRequest)
		return
	}

	// Get the authenticated user email from context (either existing or new-to-register)
	var email string
	if user := s.getUser(r); user.ID != "" {
		email = user.Email
	} else if userToRegister, ok := ctx.Value(userToRegisterContextKey).(types.User); ok {
		email = userToRegister.Email
	}

	if email == "" {
		log.Ctx(ctx).WarnContext(ctx, "unauthorized access to submit interest")
		writeJSONError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	submission := types.InterestSubmission{
		Email:               email,
		Utility:             req.Utility,
		Battery:             req.Battery,
		UtilityProviderName: req.UtilityProviderName,
		State:               req.State,
		PlanName:            req.PlanName,
		BatteryName:         req.BatteryName,
		Comments:            req.Comments,
		Timestamp:           time.Now().UTC(),
	}

	if err := s.storage.UpsertInterest(ctx, submission); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to upsert interest", slog.Any("error", err))
		writeJSONError(w, "failed to save interest", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write([]byte(`{"success":true}`)); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) handleListInterest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.getUser(r)

	if !s.isMultiSiteAdmin(user) && !s.bypassAuth {
		log.Ctx(ctx).WarnContext(ctx, "unauthorized access to list interest", slog.String("email", user.Email))
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}

	limit := 50
	limitStr := r.URL.Query().Get("limit")
	if limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	submissions, err := s.storage.ListInterest(ctx, limit)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to list interest", slog.Any("error", err))
		writeJSONError(w, "failed to list interest", http.StatusInternalServerError)
		return
	}

	if submissions == nil {
		submissions = []types.InterestSubmission{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(submissions); err != nil {
		panic(http.ErrAbortHandler)
	}
}
