package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/nociriysname/qudata-agent/internal/storage"
	"github.com/nociriysname/qudata-agent/pkg/types"
)

const (
	basePath = "https://internal.qudata.ai/v0"
)

type Client struct {
	apiKey    string
	secretKey string
	http      *retryablehttp.Client
}

func NewClient(apiKey string) (*Client, error) {
	secret, _ := storage.LoadSecretKey()

	client := retryablehttp.NewClient()
	client.RetryMax = 3
	client.RetryWaitMin = 1 * time.Second
	client.RetryWaitMax = 5 * time.Second
	client.Logger = nil

	return &Client{
		apiKey:    apiKey,
		secretKey: secret,
		http:      client,
	}, nil
}

func (c *Client) UpdateSecret(key string) {
	c.secretKey = key
}

// InitAgent регистрирует агента
func (c *Client) InitAgent(req types.InitAgentRequest) (*types.AgentResponse, error) {
	resp, err := c.do("POST", "/init", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	type responseWrapper struct {
		Ok   bool                 `json:"ok"`
		Data *types.AgentResponse `json:"data"`
	}

	var wrapper responseWrapper
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}

	if !wrapper.Ok || wrapper.Data == nil {
		return nil, fmt.Errorf("server returned ok=false")
	}

	return wrapper.Data, nil
}

// CreateHost отправляет данные аттестации
func (c *Client) CreateHost(req types.CreateHostRequest) error {
	resp, err := c.do("POST", "/init/host", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("create host failed with status: %d", resp.StatusCode)
	}
	return nil
}

// SendStats отправляет метрики
func (c *Client) SendStats(req types.StatsRequest) error {
	// Запускаем в горутине, чтобы не блокировать основной поток (fire-and-forget)
	go func() {
		resp, err := c.do("POST", "/stats", req)
		if err == nil {
			resp.Body.Close()
		}
	}()
	return nil
}

// NotifyInstanceReady сообщает, что SSH поднялся
func (c *Client) NotifyInstanceReady(instanceID string) error {
	path := fmt.Sprintf("/instances/%s/ready", instanceID)
	resp, err := c.do("POST", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// ReportIncident - ИСПРАВЛЕННЫЙ МЕТОД
// Сигнатура точно совпадает с интерфейсом incidentReporter в security
func (c *Client) ReportIncident(incidentType string, reason string) error {
	payload := map[string]interface{}{
		"type":      incidentType,
		"reason":    reason,
		"timestamp": time.Now().Unix(),
	}

	// Используем синхронный вызов, так как это важно
	resp, err := c.do("POST", "/incidents", payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("report incident failed: %d", resp.StatusCode)
	}
	return nil
}

// Внутренний метод для запросов
func (c *Client) do(method, path string, body interface{}) (*http.Response, error) {
	var buf io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewBuffer(data)
	}

	// Context Background используется внутри retryablehttp, если не передан явно
	req, err := retryablehttp.NewRequest(method, basePath+path, buf)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	if c.secretKey != "" {
		req.Header.Set("X-Agent-Secret", c.secretKey)
	} else {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	return c.http.Do(req)
}
