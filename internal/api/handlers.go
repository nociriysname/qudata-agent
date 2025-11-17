package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	agenttypes "github.com/nociriysname/qudata-agent/pkg/types"
)

type Handlers struct {
	orchestrator Orchestrator
}

type sshKeyRequest struct {
	PublicKey string `json:"public_key"`
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

	state, err := h.orchestrator.CreateInstance(r.Context(), req)
	if err != nil {
		log.Printf("ERROR: Failed to create instance: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"instance_id": state.InstanceID,
		"ports":       state.AllocatedPorts,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
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

func (h *Handlers) HandleAddSSHKey(w http.ResponseWriter, r *http.Request) {
	var req sshKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.PublicKey == "" {
		http.Error(w, "public_key field is required", http.StatusBadRequest)
		return
	}

	if err := h.orchestrator.AddSSHKey(r.Context(), req.PublicKey); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) HandleRemoveSSHKey(w http.ResponseWriter, r *http.Request) {
	var req sshKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.PublicKey == "" {
		http.Error(w, "public_key field is required", http.StatusBadRequest)
		return
	}

	if err := h.orchestrator.RemoveSSHKey(r.Context(), req.PublicKey); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) HandleListSSHKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.orchestrator.ListSSHKeys(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string][]string{"keys": keys}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (h *Handlers) HandleManageInstance(w http.ResponseWriter, r *http.Request) {
	var req agenttypes.ManageInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.orchestrator.ManageInstance(r.Context(), req.Action); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"message": "Action '%s' initiated successfully"}`, req.Action)
	w.Header().Set("Content-Type", "application/json")
}

// HandleGetInstanceLogs обрабатывает запрос на получение логов.
func (h *Handlers) HandleGetInstanceLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := h.orchestrator.GetInstanceLogs(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(logs))
}
