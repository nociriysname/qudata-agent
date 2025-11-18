package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/daemon"
	"github.com/google/uuid"
	"github.com/nociriysname/qudata-agent/internal/api"
	"github.com/nociriysname/qudata-agent/internal/attestation"
	config "github.com/nociriysname/qudata-agent/internal/cfg"
	"github.com/nociriysname/qudata-agent/internal/client"
	"github.com/nociriysname/qudata-agent/internal/orchestrator"
	"github.com/nociriysname/qudata-agent/internal/security"
	"github.com/nociriysname/qudata-agent/internal/stats"
	"github.com/nociriysname/qudata-agent/internal/storage"
	"github.com/nociriysname/qudata-agent/pkg/types"
)

const agentPort = 8080

func main() {
	if security.IsWatchdogChild() {
		runWatchdogChild()
	} else {
		runMainAgent()
	}
}

func runWatchdogChild() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Printf("FATAL [Watchdog Child]: Failed to load config: %v", err)
		os.Exit(1)
	}
	qClient, err := client.NewClient(cfg.APIKey)
	if err != nil {
		log.Printf("FATAL [Watchdog Child]: Failed to create client: %v", err)
		os.Exit(1)
	}
	orch, err := orchestrator.NewLite()
	if err != nil {
		log.Printf("FATAL [Watchdog Child]: Failed to create orchestrator: %v", err)
		os.Exit(1)
	}

	deps := security.NewLockdownDependencies(orch, qClient)
	security.RunAsChild(deps)
}

func runMainAgent() {
	logger := log.New(os.Stdout, "QUDATA-AGENT | ", log.LstdFlags)
	logger.Println("Starting main agent process...")

	if err := security.StartWatchdog(); err != nil {
		logger.Fatalf("FATAL: %v", err)
	}

	interval, err := daemon.SdWatchdogEnabled(false)
	if err == nil && interval > 0 {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				daemon.SdNotify(false, daemon.SdNotifyWatchdog)
			}
		}()
		logger.Println("Systemd watchdog enabled.")
	}

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

	logger.Println("Generating host hardware report...")
	hostReport := attestation.GenerateHostReport()
	if hostReport == nil {
		logger.Fatalf("FATAL: Could not generate host hardware report.")
	}

	initReq := types.InitAgentRequest{
		AgentID:     uuid.NewString(),
		AgentPort:   agentPort,
		Address:     getOutboundIP(),
		Fingerprint: hostReport.Fingerprint,
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

	if !agentResp.HostExists {
		logger.Println("Host not found on server. Registering new host...")

		createHostReq := types.CreateHostRequest{
			GPUName:       hostReport.GPUName,
			GPUAmount:     hostReport.GPUAmount,
			VRAM:          hostReport.VRAM,
			Location:      types.Location{},
			MaxCUDA:       hostReport.CUDAVersion,
			Configuration: hostReport.Configuration,
		}

		jsonData, _ := json.MarshalIndent(createHostReq, "", "  ")
		logger.Printf("Sending CreateHost request: %s", string(jsonData))

		if err := qClient.CreateHost(createHostReq); err != nil {
			logger.Fatalf("FATAL: Failed to register host on server: %v", err)
		}
		logger.Println("Host registered successfully.")
	}

	orch, err := orchestrator.New(qClient)
	if err != nil {
		logger.Fatalf("FATAL: Failed to initialize orchestrator: %v", err)
	}
	logger.Println("Orchestrator initialized successfully.")

	if err := orch.SyncState(context.Background()); err != nil {
		logger.Fatalf("FATAL: Failed to sync state with Docker: %v", err)
	}

	secMon, err := security.NewSecurityMonitor(orch, qClient)
	if err != nil {
		logger.Fatalf("FATAL: Failed to initialize security monitor: %v", err)
	}
	secMon.Run()
	logger.Println("Security Monitor started.")

	httpServer := api.NewServer(agentPort, orch)
	logger.Printf("API server configured on port %d.", agentPort)

	go func() {
		logger.Println("API server is starting to listen...")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("FATAL: Could not start API server: %v", err)
		}
	}()

	go func() {
		statsCollector := stats.NewCollector()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			currentState := storage.GetState()
			statsReq := types.StatsRequest{
				CPUUtil: statsCollector.GetCPUUtil(),
				RAMUtil: statsCollector.GetRAMUtil(),
				GPUUtil: statsCollector.GetGPUUtil(),
				MemUtil: statsCollector.GetGPUMemoryUtil(),
				Status:  currentState.Status,
			}
			if err := qClient.SendStats(statsReq); err != nil {
				logger.Printf("Warning: failed to send stats: %v", err)
			} else {
				logger.Println("Stats sent successfully.")
			}
		}
	}()

	daemon.SdNotify(false, daemon.SdNotifyReady)
	logger.Println("Agent is ready and running.")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Println("Shutdown signal received. Shutting down gracefully...")

	daemon.SdNotify(false, daemon.SdNotifyStopping)

	secMon.Stop()

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
	defer func() { _ = conn.Close() }()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
