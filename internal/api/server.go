package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	agenttypes "github.com/nociriysname/qudata-agent/pkg/types"
)

type Orchestrator interface {
	CreateInstance(ctx context.Context, req agenttypes.CreateInstanceRequest) (*agenttypes.InstanceState, error)
	DeleteInstance(ctx context.Context) error
	AddSSHKey(ctx context.Context, publicKey string) error
	RemoveSSHKey(ctx context.Context, publicKey string) error
	ListSSHKeys(ctx context.Context) ([]string, error)
	ManageInstance(ctx context.Context, action agenttypes.InstanceAction) error
	GetInstanceLogs(ctx context.Context) (string, error)
}

func NewServer(port int, orch Orchestrator) *http.Server {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	handlers := NewHandlers(orch)

	r.Get("/ping", handlers.HandlePing)

	r.Route("/ssh", func(r chi.Router) {
		r.Get("/", handlers.HandleListSSHKeys)
		r.Post("/", handlers.HandleAddSSHKey)
		r.Delete("/", handlers.HandleRemoveSSHKey)
	})

	r.Route("/instances", func(r chi.Router) {
		r.Post("/", handlers.HandleCreateInstance)
		r.Delete("/", handlers.HandleDeleteInstance)
		r.Put("/", handlers.HandleManageInstance)
		r.Get("/logs", handlers.HandleGetInstanceLogs)
	})

	return &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: r,
	}
}
