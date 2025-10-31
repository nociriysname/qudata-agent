package api

import (
	"encoding/json"
	"log"
	"net/http"

	agenttypes "github.com/nociriysname/qudata-agent/pkg/types"
)

type Handlers struct {
	orchestrator Orchestrator
}

func NewHandlers(orch Orchestrator) *Handlers {
	return &Handlers{orchestrator: orch}
}

func (h *Handlers) HandleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var req agenttypes.CreateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	go func() {
		log.Printf("Starting to create instance for image %s:%s...", req.Image, req.ImageTag)
		if err := h.orchestrator.CreateInstance(r.Context(), req); err != nil {
			log.Printf("ERROR: Failed to create instance asynchronously: %v", err)
		} else {
			log.Println("Instance created successfully in background.")
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"message": "Instance creation started"}`))
	w.Header().Set("Content-Type", "application/json")
}

func (h *Handlers) HandleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	go func() {
		log.Println("Starting to delete instance...")
		if err := h.orchestrator.DeleteInstance(r.Context()); err != nil {
			log.Printf("ERROR: Failed to delete instance asynchronously: %v", err)
		} else {
			log.Println("Instance deleted successfully in background.")
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"message": "Instance deletion started"}`))
	w.Header().Set("Content-Type", "application/json")
}

func (h *Handlers) HandlePing(w http.ResponseWriter, r *http.Request) {
	response := map[string]bool{"ok": true}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}
