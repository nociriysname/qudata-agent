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

type apiResponse struct {
	Ok   bool                `json:"ok"`
	Data types.AgentResponse `json:"data"`
}

func NewClient(apiKey string) (*QudataClient, error) {
	secret, err := storage.LoadSecretKey()
	if err != nil {
		log.Printf("Info: starting without loaded secret key")
	}

	client := &QudataClient{
		httpClient: &http.Client{Timeout: requestTimeout},
		baseURL:    qudataAPIBaseURL,
		apiKey:     apiKey,
		secretKey:  secret,
	}

	if secret != "" {
		client.apiKey = ""
	}

	return client, nil
}

func (c *QudataClient) UpdateSecret(key string) {
	if key != "" {
		c.secretKey = key
		c.apiKey = ""
	}
}

func (c *QudataClient) doRequest(method, path string, body any) (*http.Response, error) {
	var buf io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewBuffer(jsonData)
	}

	url := fmt.Sprintf("%s%s", c.baseURL, path)
	req, err := http.NewRequest(method, url, buf)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	if c.secretKey != "" {
		req.Header.Set("X-Agent-Secret", c.secretKey)
	}

	return c.httpClient.Do(req)
}

func checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body = io.NopCloser(bytes.NewBuffer(body))
	return fmt.Errorf("status: %d, body: %s", resp.StatusCode, string(body))
}

func (c *QudataClient) InitAgent(req types.InitAgentRequest) (*types.AgentResponse, error) {
	resp, err := c.doRequest("POST", "/init", req)
	if err != nil {
		return nil, fmt.Errorf("failed to send init request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	log.Printf("DEBUG RAW INIT RESPONSE: %s", string(bodyBytes))

	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned non-200 status for init: %d", resp.StatusCode)
	}

	var wrapper apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("failed to decode server response: %w", err)
	}

	if wrapper.Data.SecretKey != "" {
		c.UpdateSecret(wrapper.Data.SecretKey)
	}

	return &wrapper.Data, nil
}

func (c *QudataClient) CreateHost(req types.CreateHostRequest) error {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal create host request: %w", err)
	}

	url := fmt.Sprintf("%s/init/host", c.baseURL)

	httpReq, err := c.newRequest("POST", url, reqBody, false)
	if err != nil {
		return fmt.Errorf("failed to create http request for create host: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send create host request: %w", err)
	}
	defer resp.Body.Close()

	if err := checkResponse(resp); err != nil {
		return fmt.Errorf("create host failed: %w", err)
	}

	return nil
}

func (c *QudataClient) newRequest(method, url string, body []byte, useApiKey bool) (*http.Request, error) {
	req, err := http.NewRequest(method, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	if useApiKey {
		req.Header.Set("X-Api-Key", c.apiKey)
		if c.secretKey != "" {
			req.Header.Set("X-Agent-Secret", c.secretKey)
		}
	} else {
		if c.secretKey == "" {
			return nil, fmt.Errorf("agent secret key is missing")
		}
		req.Header.Set("X-Agent-Secret", c.secretKey)
	}
	return req, nil
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

	resp, err := c.doRequest("POST", "/incidents", incidentPayload)
	if err != nil {
		return fmt.Errorf("failed to send incident report: %w", err)
	}
	defer resp.Body.Close()

	return checkResponse(resp)
}

func (c *QudataClient) SendStats(req types.StatsRequest) error {
	resp, err := c.doRequest("POST", "/stats", req)
	if err != nil {
		return fmt.Errorf("failed to send stats: %w", err)
	}
	defer resp.Body.Close()

	return checkResponse(resp)
}

func (c *QudataClient) NotifyInstanceReady(instanceID string) error {
	path := fmt.Sprintf("/instances/%s/ready", instanceID)
	resp, err := c.doRequest("POST", path, nil)
	if err != nil {
		return fmt.Errorf("failed to send ready notification: %w", err)
	}
	defer resp.Body.Close()

	return checkResponse(resp)
}
