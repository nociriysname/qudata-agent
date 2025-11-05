//go:build linux

package security

import (
	"fmt"
	"log"
	"strings"

	"github.com/elastic/go-libaudit/v2"
	"github.com/elastic/go-libaudit/v2/auparse"
	"github.com/elastic/go-libaudit/v2/rule"
)

var forbiddenCommands = []string{
	"/usr/bin/virsh",
	"/usr/bin/qemu-img",
	"/usr/bin/qemu-io",
	"/usr/bin/pcileech",
	"/usr/bin/memdump",
}

type AuditMonitor struct {
	client       *libaudit.AuditClient
	stopChan     chan struct{}
	stoppedChan  chan struct{}
	lockdownDeps LockdownDependencies
}

func NewAuditMonitor() (*AuditMonitor, error) {
	client, err := libaudit.NewAuditClient(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit client: %w", err)
	}

	delRule := &rule.DeleteAllRule{
		Type: rule.DeleteAllRuleType,
		Keys: []string{"qudata_exec_watch"},
	}
	delBuf, err := rule.Build(delRule)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to build delete rule: %w", err)
	}

	if err := client.DeleteRule(delBuf); err != nil {
		log.Printf("Warning: failed to delete old audit rules: %v", err)
	}

	for _, cmdPath := range forbiddenCommands {
		syscallRule := &rule.SyscallRule{
			Type:   rule.AppendSyscallRuleType,
			List:   "always",
			Action: "exit",
			Filters: []rule.FilterSpec{
				{Type: rule.ValueFilterType, LHS: "path", Comparator: "=", RHS: cmdPath},
				{Type: rule.ValueFilterType, LHS: "perm", Comparator: "=", RHS: "x"},
			},
			Syscalls: []string{"execve"},
			Keys:     []string{"qudata_exec_watch"},
		}
		buf, err := rule.Build(syscallRule)
		if err != nil {
			client.Close()
			return nil, fmt.Errorf("failed to build audit rule for %s: %w", cmdPath, err)
		}
		if err := client.AddRule(buf); err != nil {
			client.Close()
			return nil, fmt.Errorf("failed to add audit rule for %s: %w", cmdPath, err)
		}
	}

	log.Println("[Security] Auditd rules for forbidden commands have been set.")

	return &AuditMonitor{
		client:      client,
		stopChan:    make(chan struct{}),
		stoppedChan: make(chan struct{}),
	}, nil
}

func (m *AuditMonitor) Start(deps LockdownDependencies) {
	m.lockdownDeps = deps
	log.Println("[Security] Starting auditd event listener...")
	go m.runLoop()
}

func (m *AuditMonitor) Stop() {
	log.Println("[Security] Stopping auditd event listener...")
	close(m.stopChan)
	m.client.Close()
	<-m.stoppedChan
	log.Println("[Security] Auditd event listener stopped.")
}

func (m *AuditMonitor) runLoop() {
	defer close(m.stoppedChan)

	for {
		rawEvent, err := m.client.Receive(false)
		if err != nil {
			select {
			case <-m.stopChan:
				return
			default:
				log.Printf("ERROR: Audit receive failed: %v", err)
				return
			}
		}

		if rawEvent.Type == auparse.AUDIT_SYSCALL {
			auditMsg, err := auparse.ParseLogLine(string(rawEvent.Data))
			if err != nil {
				log.Printf("Warning: failed to parse audit message: %v", err)
				continue
			}

			data, err := auditMsg.Data()
			if err != nil {
				log.Printf("Warning: failed to extract data from audit message: %v", err)
				continue
			}

			key := data["key"]
			exe := data["exe"]

			if key == `"qudata_exec_watch"` {
				exe = strings.Trim(exe, `"`)
				for _, forbidden := range forbiddenCommands {
					if exe == forbidden {
						reason := fmt.Sprintf("Forbidden command executed: %s", exe)
						log.Printf("!!! SECURITY ALERT [auditd] !!! Forbidden command executed: %s", reason)
						go InitiateLockdown(m.lockdownDeps, reason)
						break
					}
				}
			}
		}
	}
}
