//go:build linux

package security

import (
	"context"
	_ "fmt"
	"log"
	"os"

	"github.com/nociriysname/qudata-agent/internal/storage"
)

const lockdownFilePath = "/var/lib/qudata/lockdown.lock"

type LockdownDependencies interface {
	DeleteInstance(ctx context.Context) error
	ReportIncident(incidentType, reason string) error
}

func InitiateLockdown(deps LockdownDependencies, reason string) {
	log.Printf("!!! CRITICAL SECURITY THREAT DETECTED !!! Reason: %s", reason)
	log.Println("!!! INITIATING EMERGENCY LOCKDOWN !!!")

	// 1. Создаем lock-файл, чтобы предотвратить автоматический перезапуск агента.
	log.Println("Creating lockdown file...")
	file, err := os.Create(lockdownFilePath)
	if err != nil {
		log.Printf("ERROR: Failed to create lockdown file: %v", err)
	} else {
		file.Close()
		log.Printf("Lockdown file created at %s", lockdownFilePath)
	}

	// 2. Отправляем отчет об инциденте на сервер кудаты.
	log.Println("Reporting incident to Qudata server...")
	if err := deps.ReportIncident("security_breach", reason); err != nil {
		log.Printf("ERROR: Failed to report incident to server: %v", err)
	}

	// 3. Немедленно уничтожаем текущий инстанс, если он существует.
	log.Println("Force-deleting active instance...")
	if err := deps.DeleteInstance(context.Background()); err != nil {
		log.Printf("ERROR: Emergency instance deletion failed: %v", err)
	} else {
		log.Println("Instance destroyed.")
	}

	// 4. Безопасно удаляем локальные секреты.
	log.Println("Shredding local secrets...")
	if err := storage.ShredSecretFile(); err != nil {
		log.Printf("ERROR: Failed to shred secret file: %v", err)
	} else {
		log.Println("Local secrets shredded.")
	}

	log.Println("Forcing agent termination.")
	os.Exit(1)
}
