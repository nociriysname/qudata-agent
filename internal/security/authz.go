//go:build linux

package security

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	// Путь к сокету
	authzSocketPath = "/run/docker/plugins/qudata-authz.sock"
)

// Запрещенные эндпоинты Docker API. Любой запрос, содержащий эти строки, будет заблокирован.
var forbiddenDockerEndpoints = []string{
	"/exec",
	"/attach",
	"/copy",
	"/archive",
	"/commit",
	"/rename",
	"/update",
	"/kill",
}

type AuthzPlugin struct {
	listener net.Listener
}

// NewAuthzPlugin создает и инициализирует плагин.
func NewAuthzPlugin() (*AuthzPlugin, error) {
	// Удаляем старый сокет, если он существует.
	if err := os.RemoveAll(authzSocketPath); err != nil {
		return nil, fmt.Errorf("failed to remove old authz socket: %w", err)
	}

	// Создаем родительскую директорию для сокета.
	if err := os.MkdirAll(filepath.Dir(authzSocketPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create authz socket directory: %w", err)
	}

	// Начинаем слушать Unix-сокет.
	listener, err := net.Listen("unix", authzSocketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on authz socket: %w", err)
	}

	return &AuthzPlugin{listener: listener}, nil
}

// Start запускает сервер плагина в отдельной горутине.
func (p *AuthzPlugin) Start() {
	log.Printf("[Security] Starting Docker authz plugin on socket %s", authzSocketPath)

	mux := http.NewServeMux()
	mux.HandleFunc("/Plugin.Activate", p.handleActivate)
	mux.HandleFunc("/AuthZPlugin.Allow", p.handleAllow)

	go func() {
		// Игнорируем ошибку "use of closed network connection", которая возникает при штатной остановке.
		if err := http.Serve(p.listener, mux); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			log.Printf("ERROR: Docker authz plugin server failed: %v", err)
		}
	}()
}

// Stop корректно останавливает сервер плагина.
func (p *AuthzPlugin) Stop() {
	log.Println("[Security] Stopping Docker authz plugin...")
	if p.listener != nil {
		p.listener.Close()
	}
}

// handleActivate отвечает на "пинг" от Docker daemon при его запуске.
func (p *AuthzPlugin) handleActivate(w http.ResponseWriter, r *http.Request) {
	response := map[string][]string{
		"Implements": {"authz"},
	}
	json.NewEncoder(w).Encode(response)
}

// authzRequest - структура для парсинга запроса от Docker daemon.
type authzRequest struct {
	RequestMethod string `json:"RequestMethod"`
	RequestUri    string `json:"RequestUri"`
	User          string `json:"User"`
}

// authzResponse - структура для ответа Docker daemon'у.
type authzResponse struct {
	Allow bool   `json:"Allow"`
	Msg   string `json:"Msg,omitempty"`
}

// handleAllow - это главный метод, который принимает решение о блокировке.
func (p *AuthzPlugin) handleAllow(w http.ResponseWriter, r *http.Request) {
	var req authzRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		p.respond(w, false, "Invalid request from Docker daemon")
		return
	}

	// Проверяем, содержит ли URI запроса что-либо из нашего черного списка.
	for _, endpoint := range forbiddenDockerEndpoints {
		if strings.Contains(req.RequestUri, endpoint) {
			log.Printf("!!! SECURITY ALERT [authz] !!! DENIED dangerous Docker API call from user '%s': %s %s",
				req.User, req.RequestMethod, req.RequestUri)
			p.respond(w, false, "Action denied by Qudata Agent security policy.")
			return
		}
	}

	// Если ничего опасного не найдено, разрешаем запрос.
	log.Printf("[Security] [authz] ALLOWED Docker API call: %s %s", req.RequestMethod, req.RequestUri)
	p.respond(w, true, "")
}

func (p *AuthzPlugin) respond(w http.ResponseWriter, allow bool, msg string) {
	w.Header().Set("Content-Type", "application/json")
	response := authzResponse{Allow: allow, Msg: msg}
	json.NewEncoder(w).Encode(response)
}
