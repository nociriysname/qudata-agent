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
	// Защита агента: если запущен как дочерний процесс для слежения
	if security.IsWatchdogChild() {
		runWatchdogChild()
	} else {
		runMainAgent()
	}
}

func runWatchdogChild() {
	// Загружаем конфиг для восстановления
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Printf("FATAL [Watchdog]: Failed to load config: %v", err)
		os.Exit(1)
	}
	qClient, err := client.NewClient(cfg.APIKey)
	if err != nil {
		log.Printf("FATAL [Watchdog]: Failed to create client: %v", err)
		os.Exit(1)
	}

	// Инициализируем оркестратор для экстренного удаления
	orch, err := orchestrator.New(qClient)
	if err != nil {
		log.Printf("FATAL [Watchdog]: Failed to create orchestrator: %v", err)
		os.Exit(1)
	}

	// Запускаем слежение за основным процессом
	deps := security.NewLockdownDependencies(orch, qClient)
	security.RunAsChild(deps)
}

func runMainAgent() {
	logger := log.New(os.Stdout, "QUDATA-AGENT | ", log.LstdFlags)
	logger.Println(">>> QuData Agent (Bare Metal + Kata + CGO) Starting...")

	// 1. Запуск Watchdog (родительский процесс следит за нами)
	if err := security.StartWatchdog(); err != nil {
		logger.Fatalf("FATAL: Watchdog failed: %v", err)
	}

	// Уведомляем systemd (если есть)
	interval, err := daemon.SdWatchdogEnabled(false)
	if err == nil && interval > 0 {
		go func() {
			ticker := time.NewTicker(interval / 2)
			defer ticker.Stop()
			for range ticker.C {
				daemon.SdNotify(false, daemon.SdNotifyWatchdog)
			}
		}()
		logger.Println("Systemd watchdog enabled.")
	}

	// 2. Конфигурация и Состояние
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Fatalf("FATAL: Config error: %v", err)
	}

	if err := storage.LoadState(); err != nil {
		logger.Fatalf("FATAL: State load error: %v", err)
	}
	logger.Printf("State loaded. Status: %s", storage.GetState().Status)

	// 3. Клиент API
	qClient, err := client.NewClient(cfg.APIKey)
	if err != nil {
		logger.Fatalf("FATAL: Client error: %v", err)
	}

	// 4. АТТЕСТАЦИЯ (CGO)
	logger.Println("Running Hardware Attestation (CGO)...")
	hostReport := attestation.GenerateHostReport()
	if hostReport == nil {
		logger.Fatalf("FATAL: Hardware attestation failed.")
	}

	// 5. Инициализация на бэкенде
	initReq := types.InitAgentRequest{
		AgentID:     uuid.NewString(), // В идеале читать из storage.GetAgentID()
		AgentPort:   agentPort,
		Address:     getOutboundIP(),
		Fingerprint: hostReport.Fingerprint,
		PID:         os.Getpid(),
	}

	logger.Println("Registering agent...")
	agentResp, err := qClient.InitAgent(initReq)
	if err != nil {
		logger.Fatalf("FATAL: Init failed: %v", err)
	}
	logger.Printf("Agent registered. Host exists: %v", agentResp.HostExists)

	// Сохраняем секретный ключ
	if agentResp.SecretKey != "" {
		if err := storage.SaveSecretKey(agentResp.SecretKey); err != nil {
			logger.Fatalf("FATAL: Save secret failed: %v", err)
		}
		qClient.UpdateSecret(agentResp.SecretKey)
		logger.Println("Secret key updated.")
	}

	// Если хост новый - регистрируем железо
	if !agentResp.HostExists {
		logger.Println("Registering new host hardware...")

		createHostReq := types.CreateHostRequest{
			GPUName:       hostReport.GPUName,
			GPUAmount:     hostReport.GPUAmount,
			VRAM:          hostReport.VRAM,
			MaxCUDA:       hostReport.CUDAVersion,
			Configuration: hostReport.Configuration,
		}

		// Логгируем JSON для отладки
		jsonData, _ := json.MarshalIndent(createHostReq, "", "  ")
		logger.Printf("Host Payload: %s", string(jsonData))

		if err := qClient.CreateHost(createHostReq); err != nil {
			logger.Fatalf("FATAL: Host registration failed: %v", err)
		}
		logger.Println("Host registered successfully.")
	}

	// 6. Оркестратор (Kata + GPU)
	orch, err := orchestrator.New(qClient)
	if err != nil {
		logger.Fatalf("FATAL: Orchestrator init failed: %v", err)
	}

	// Синхронизация состояния (если агент перезапустился)
	if err := orch.SyncState(context.Background()); err != nil {
		logger.Printf("Warning: State sync failed: %v", err)
	}

	// 7. Монитор безопасности (Auditd, AuthZ)
	secMon, err := security.NewSecurityMonitor(orch, qClient)
	if err != nil {
		logger.Fatalf("FATAL: Security monitor failed: %v", err)
	}
	secMon.Run()
	logger.Println("Security Monitor active.")

	// 8. HTTP Сервер
	httpServer := api.NewServer(agentPort, orch)
	go func() {
		logger.Printf("API listening on :%d", agentPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("FATAL: HTTP Server crashed: %v", err)
		}
	}()

	// 9. Сбор и отправка статистики (Фоновый процесс)
	go func() {
		statsCollector := stats.NewCollector()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			// Используем единый метод Collect(), который внутри дергает CGO для GPU
			snapshot := statsCollector.Collect()
			snapshot.Status = storage.GetState().Status

			if err := qClient.SendStats(snapshot); err != nil {
				logger.Printf("Stats send error: %v", err)
			}
		}
	}()

	// Готовность
	daemon.SdNotify(false, daemon.SdNotifyReady)
	logger.Println(">>> AGENT IS READY AND RUNNING <<<")

	// Ожидание выхода
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Println("Shutdown signal received...")
	daemon.SdNotify(false, daemon.SdNotifyStopping)
	secMon.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpServer.Shutdown(ctx)

	logger.Println("Goodbye.")
}

// getOutboundIP определяет внешний IP для регистрации
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer func() { _ = conn.Close() }()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
