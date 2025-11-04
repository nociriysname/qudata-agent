//go:build linux

package security

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"
)

const (
	watchdogEnvVar       = "QUDATA_WATCHDOG_CHILD"
	watchdogTimeout      = 15 * time.Second
	watchdogPingInterval = 5 * time.Second
)

func IsWatchdogChild() bool {
	return os.Getenv(watchdogEnvVar) == "1"
}

func RunAsChild(deps lockdownDependencies) {
	log.Println("[Watchdog] Running as child process. Monitoring parent...")

	stdin := os.Stdin
	_ = stdin.SetReadDeadline(time.Now().Add(watchdogTimeout))

	buf := make([]byte, 1)
	for {
		_, err := stdin.Read(buf)
		if err != nil {
			if os.IsTimeout(err) {
				reason := fmt.Sprintf("Parent process heartbeat timeout after %v", watchdogTimeout)
				InitiateLockdown(deps, reason)
			}
			log.Printf("[Watchdog] Pipe closed or read error: %v. Child process exiting.", err)
			return
		}
		_ = stdin.SetReadDeadline(time.Now().Add(watchdogTimeout))
	}
}

func StartWatchdog() error {
	selfPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not find path to own executable: %w", err)
	}

	cmd := exec.Command(selfPath)
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=1", watchdogEnvVar))

	pipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("could not create watchdog pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start watchdog child process: %w", err)
	}
	log.Printf("[Watchdog] Child process started with PID %d", cmd.Process.Pid)

	go pingChild(pipe)

	return nil
}

func pingChild(pipe io.WriteCloser) {
	ticker := time.NewTicker(watchdogPingInterval)
	defer ticker.Stop()

	for range ticker.C {
		if _, err := pipe.Write([]byte{'.'}); err != nil {
			log.Printf("[Watchdog] Failed to ping child process (pipe closed): %v", err)
			return
		}
	}
}
