package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/nociriysname/qudata-agent/internal/api"
	"github.com/nociriysname/qudata-agent/internal/cfg"
	"github.com/nociriysname/qudata-agent/internal/client"
	"github.com/nociriysname/qudata-agent/internal/orchestrator"
	"github.com/nociriysname/qudata-agent/internal/storage"
	"github.com/nociriysname/qudata-agent/pkg/types"
)

const agentPort = 8080

func main() {
	logger := log.New(os.Stdout, "QUDATA-AGENT | ", log.LstdFlags)
	logger.Println("Starting agent...")

	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Fatalf("FATAL: Failed to load configuration: %v", err)
	}

	if err := storage.LoadState(); err != nil {
		logger.Fatalf("FATAL: Failed to load initial state: %v", err)
	}
	logger.Printf("Initial state loaded. Current status: %s", storage.GetState().Status)

	qClient, err := client.NewClient(cfg.APIKey)
	if err != nil {
		logger.Fatalf("FATAL: Failed to create qudata client: %v", err)
	}

	initReq := types.InitAgentRequest{
		AgentID:     uuid.NewString(),
		AgentPort:   agentPort,
		Address:     getOutboundIP(),
		Fingerprint: "placeholder-fingerprint", // TODO: сделаем в следующих итерациях
		PID:         os.Getpid(),
	}

	logger.Println("Initializing agent on Qudata server...")
	agentResp, err := qClient.InitAgent(initReq)
	if err != nil {
		logger.Fatalf("FATAL: Failed to initialize agent on server: %v", err)
	}
	logger.Printf("Agent initialized successfully. Host exists: %v", agentResp.HostExists)

	if agentResp.SecretKey != "" {
		if err := storage.SaveSecretKey(agentResp.SecretKey); err != nil {
			logger.Fatalf("FATAL: Failed to save new secret key: %v", err)
		}
		qClient.UpdateSecret(agentResp.SecretKey)
		logger.Println("New secret key saved and activated.")
	}

	orch, err := orchestrator.New()
	if err != nil {
		logger.Fatalf("FATAL: Failed to initialize orchestrator: %v", err)
	}
	logger.Println("Orchestrator initialized successfully.")

	httpServer := api.NewServer(agentPort, orch)
	logger.Printf("API server configured on port %d.", agentPort)

	go func() {
		logger.Println("API server is starting to listen...")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("FATAL: Could not start API server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Println("Shutdown signal received. Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Fatalf("FATAL: Server forced to shutdown: %v", err)
	}

	logger.Println("Agent shut down successfully.")
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
