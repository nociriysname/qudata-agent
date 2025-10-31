package client

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	secret, err := storage.LoadSecretKey()
	if err != nil {
		return nil, fmt.Errorf("could not load secret key: %w", err)
	}

	return &QudataClient{
		httpClient: &http.Client{Timeout: requestTimeout},
		baseURL:    qudataAPIBaseURL,
		apiKey:     apiKey,
		secretKey:  secret,
	}, nil
}

func (c *QudataClient) UpdateSecret(key string) {
	c.secretKey = key
}

func (c *QudataClient) InitAgent(req types.InitAgentRequest) (*types.AgentResponse, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal init request: %w", err)
	}

	url := fmt.Sprintf("%s/init", c.baseURL)
	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Api-Key", c.apiKey)
	if c.secretKey != "" {
		httpReq.Header.Set("X-Agent-Secret", c.secretKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send init request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("server returned non-2xx status: %d", resp.StatusCode)
	}

	var agentResp types.AgentResponse
	if err := json.NewDecoder(resp.Body).Decode(&agentResp); err != nil {
		return nil, fmt.Errorf("failed to decode server response: %w", err)
	}

	return &agentResp, nil
}
