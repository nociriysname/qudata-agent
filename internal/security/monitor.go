//go:build linux

package security

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nociriysname/qudata-agent/internal/storage"
)

type incidentReporter interface {
	ReportIncident(incidentType, reason string) error
}

type instanceDeleter interface {
	DeleteInstance(ctx context.Context) error
}

type SecurityMonitor struct {
	fanotifyMon  *FanotifyMonitor
	auditMon     *AuditMonitor
	authzPlugin  *AuthzPlugin
	mu           sync.Mutex
	orchestrator instanceDeleter
	client       incidentReporter
}

func NewSecurityMonitor(orch instanceDeleter, cli incidentReporter) (*SecurityMonitor, error) {
	auditMon, err := NewAuditMonitor()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize audit monitor: %w", err)
	}

	authzPlugin, err := NewAuthzPlugin()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize authz plugin: %w", err)
	}

	return &SecurityMonitor{
		auditMon:     auditMon,
		authzPlugin:  authzPlugin,
		orchestrator: orch,
		client:       cli,
	}, nil
}

func (sm *SecurityMonitor) DeleteInstance(ctx context.Context) error {
	return sm.orchestrator.DeleteInstance(ctx)
}

func (sm *SecurityMonitor) ReportIncident(incidentType, reason string) error {
	return sm.client.ReportIncident(incidentType, reason)
}

func (sm *SecurityMonitor) Run() {
	log.Println("[Security] Security Monitor is running...")
	sm.auditMon.Start(sm)
	sm.authzPlugin.Start()
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			sm.reconcileFanotify()
		}
	}()
}

func (sm *SecurityMonitor) Stop() {
	log.Println("[Security] Stopping all security modules...")
	sm.authzPlugin.Stop()
	sm.auditMon.Stop()
	sm.mu.Lock()
	if sm.fanotifyMon != nil {
		sm.fanotifyMon.Stop()
		sm.fanotifyMon = nil
	}
	sm.mu.Unlock()
}

// reconcileFanotify - это главный цикл сверки для fanotify. Он смотрит на текущее состояние
// и решает, нужно ли включить или выключить защиту.
func (sm *SecurityMonitor) reconcileFanotify() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state := storage.GetState()

	// Сценарий 1: Инстанс работает, а защита fanotify - нет. Нужно включить.
	if state.Status == "running" && sm.fanotifyMon == nil {
		log.Println("[Security] Instance detected. Attempting to start fanotify protection...")

		qemuPID, err := findQemuPID(state.ContainerID)
		if err != nil {
			log.Printf("ERROR: Could not find QEMU PID for container %s: %v. Retrying...", state.ContainerID, err)
			return
		}

		mon, err := NewFanotifyMonitor(state.LuksDevicePath, qemuPID, sm)
		if err != nil {
			log.Printf("ERROR: Failed to create fanotify monitor: %v", err)
			return
		}
		mon.Start()
		sm.fanotifyMon = mon
	}

	// Сценарий 2: Инстанс не работает, а защита fanotify - все еще включена. Нужно выключить.
	if state.Status != "running" && sm.fanotifyMon != nil {
		log.Println("[Security] Instance is not running. Stopping fanotify protection...")
		sm.fanotifyMon.Stop()
		sm.fanotifyMon = nil
	}
}

// findQemuPID ищет PID процесса qemu-system-*, в командной строке которого упоминается ID нашего контейнера.
func findQemuPID(containerID string) (int, error) {
	if len(containerID) < 12 {
		return 0, fmt.Errorf("container ID is too short")
	}
	shortID := containerID[:12]

	// Используем pgrep для поиска.
	cmd := exec.Command("pgrep", "-f", fmt.Sprintf("qemu-system-.*%s", shortID))
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("pgrep failed: %w, output: %s", err, string(output))
	}

	pidStr := strings.TrimSpace(string(output))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse PID from pgrep output: %s", pidStr)
	}

	return pid, nil
}

type lockdownDepsImpl struct {
	orch instanceDeleter
	cli  incidentReporter
}

func (d *lockdownDepsImpl) DeleteInstance(ctx context.Context) error {
	return d.orch.DeleteInstance(ctx)
}

func (d *lockdownDepsImpl) ReportIncident(incidentType, reason string) error {
	return d.cli.ReportIncident(incidentType, reason)
}

func NewLockdownDependencies(orch instanceDeleter, cli incidentReporter) lockdownDependencies {
	return &lockdownDepsImpl{orch: orch, cli: cli}
}
