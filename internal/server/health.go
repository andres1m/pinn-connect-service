package server

import (
	"encoding/json"
	"net/http"
	"pinn/internal/domain"
)

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	res := domain.HealthResponse{Status: "ok"}

	resBytes, err := json.Marshal(res)
	if err != nil {
		panic("")
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(resBytes)
}
