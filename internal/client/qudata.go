package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/nociriysname/qudata-agent/internal/storage"
	"github.com/nociriysname/qudata-agent/pkg/types"
)

const (
	qudataAPIBaseURL = "https://internal.qudata.ai/v0"
	requestTimeout   = 15 * time.Second
)

type QudataClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	secretKey  string
}

func NewClient(apiKey string) (*QudataClient, error) {
	// Пытаемся загрузить существующий секрет для возможного re-init
	secret, err := storage.LoadSecretKey()
	if err != nil {
		// Это нормально для первого запуска
		log.Printf("Info: starting without loaded secret key (first run or reset)")
	}

	return &QudataClient{
		httpClient: &http.Client{Timeout: requestTimeout},
		baseURL:    qudataAPIBaseURL,
		apiKey:     apiKey,
		secretKey:  secret,
	}, nil
}

// UpdateSecret обновляет секретный ключ в памяти клиента.
// Вызывается из main.go сразу после успешного InitAgent.
func (c *QudataClient) UpdateSecret(key string) {
	c.secretKey = key
}

// newRequest создает HTTP запрос с правильными заголовками в зависимости от типа запроса.
func (c *QudataClient) newRequest(method, url string, body []byte, isInit bool) (*http.Request, error) {
	var buf io.Reader
	if body != nil {
		buf = bytes.NewBuffer(body)
	}

	req, err := http.NewRequest(method, url, buf)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	if isInit {
		req.Header.Set("X-Api-Key", c.apiKey)
		if c.secretKey != "" {
			req.Header.Set("X-Agent-Secret", c.secretKey)
		}
	} else {
		if c.secretKey == "" {
			return nil, fmt.Errorf("cannot perform non-init request: agent secret key is missing")
		}
		req.Header.Set("X-Agent-Secret", c.secretKey)
	}

	return req, nil
}

func (c *QudataClient) InitAgent(req types.InitAgentRequest) (*types.AgentResponse, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal init request: %w", err)
	}

	url := fmt.Sprintf("%s/init", c.baseURL)
	// isInit = true
	httpReq, err := c.newRequest("POST", url, reqBody, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send init request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned non-200 status for init: %d", resp.StatusCode)
	}

	var agentResp types.AgentResponse
	if err := json.NewDecoder(resp.Body).Decode(&agentResp); err != nil {
		return nil, fmt.Errorf("failed to decode server response: %w", err)
	}

	return &agentResp, nil
}

func (c *QudataClient) CreateHost(req types.CreateHostRequest) error {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal create host request: %w", err)
	}

	url := fmt.Sprintf("%s/init/host", c.baseURL)
	// isInit = false (используем новый секрет)
	httpReq, err := c.newRequest("POST", url, reqBody, false)
	if err != nil {
		return fmt.Errorf("failed to create http request for create host: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send create host request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned non-200 status for create host: %d", resp.StatusCode)
	}

	return nil
}

func (c *QudataClient) ReportIncident(incidentType, reason string) error {
	incidentPayload := struct {
		IncidentType    string `json:"incident_type"`
		Timestamp       int64  `json:"timestamp"`
		InstancesKilled bool   `json:"instances_killed"`
	}{
		IncidentType:    incidentType,
		Timestamp:       time.Now().Unix(),
		InstancesKilled: true,
	}

	reqBody, err := json.Marshal(incidentPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal incident payload: %w", err)
	}

	url := fmt.Sprintf("%s/incidents", c.baseURL)
	// isInit = false
	httpReq, err := c.newRequest("POST", url, reqBody, false)
	if err != nil {
		return fmt.Errorf("failed to create incident request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send incident report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned non-2xx status for incident report: %d", resp.StatusCode)
	}

	return nil
}

func (c *QudataClient) SendStats(req types.StatsRequest) error {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal stats request: %w", err)
	}

	url := fmt.Sprintf("%s/stats", c.baseURL)
	// isInit = false
	httpReq, err := c.newRequest("POST", url, reqBody, false)
	if err != nil {
		return fmt.Errorf("failed to create stats request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned non-2xx status for stats: %d", resp.StatusCode)
	}

	return nil
}

func (c *QudataClient) NotifyInstanceReady(instanceID string) error {
	url := fmt.Sprintf("%s/instances/%s/ready", c.baseURL, instanceID)
	// isInit = false, тело nil
	httpReq, err := c.newRequest("POST", url, nil, false)
	if err != nil {
		return fmt.Errorf("failed to create instance ready request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send instance ready notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned non-2xx status for instance ready notification: %d", resp.StatusCode)
	}

	log.Printf("Successfully notified server that instance %s is ready for SSH.", instanceID)
	return nil
}
