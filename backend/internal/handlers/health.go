package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"nullpointer/backend/internal/normalizer"
)

type HealthHandler struct {
	ML *normalizer.Client
}

func NewHealthHandler(ml *normalizer.Client) *HealthHandler {
	return &HealthHandler{ML: ml}
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	defer cancel()

	mlStatus := "ok"
	if err := h.ML.Ping(ctx); err != nil {
		mlStatus = "unavailable"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"ml":     mlStatus,
	})
}
