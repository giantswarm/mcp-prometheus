package prometheus

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

// Client wraps HTTP client functionality for Prometheus API calls
type Client struct {
	httpClient *http.Client
	config     server.PrometheusConfig
	logger     server.Logger
}

// NewClient creates a new Prometheus client
func NewClient(config server.PrometheusConfig, logger server.Logger) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		config: config,
		logger: logger,
	}
}

// PrometheusResponse represents the standard Prometheus API response structure
type PrometheusResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// makeRequest makes an authenticated HTTP request to the Prometheus API
func (c *Client) makeRequest(endpoint string, params map[string]string) (*PrometheusResponse, error) {
	baseURL := strings.TrimRight(c.config.URL, "/")
	fullURL := fmt.Sprintf("%s/api/v1/%s", baseURL, endpoint)

	// Create request with parameters
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add query parameters
	if len(params) > 0 {
		q := url.Values{}
		for key, value := range params {
			q.Add(key, value)
		}
		req.URL.RawQuery = q.Encode()
	}

	// Add authentication
	c.addAuthentication(req)

	// Add organization ID header if specified
	if c.config.OrgID != "" {
		req.Header.Set("X-Scope-OrgID", c.config.OrgID)
	}

	// Make the request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var promResp PrometheusResponse
	if err := json.Unmarshal(body, &promResp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// Check Prometheus API status
	if promResp.Status != "success" {
		return nil, fmt.Errorf("Prometheus API error: %s", promResp.Error)
	}

	return &promResp, nil
}

// addAuthentication adds authentication headers or basic auth to the request
func (c *Client) addAuthentication(req *http.Request) {
	if c.config.Token != "" {
		// Bearer token authentication
		req.Header.Set("Authorization", "Bearer "+c.config.Token)
		c.logger.Debug("Using bearer token authentication")
	} else if c.config.Username != "" && c.config.Password != "" {
		// Basic authentication
		req.SetBasicAuth(c.config.Username, c.config.Password)
		c.logger.Debug("Using basic authentication", "username", c.config.Username)
	} else {
		c.logger.Debug("No authentication configured")
	}
}

// QueryResult represents the result of an instant query
type QueryResult struct {
	ResultType string      `json:"resultType"`
	Result     interface{} `json:"result"`
}

// ExecuteQuery executes an instant PromQL query
func (c *Client) ExecuteQuery(query string, timeParam string) (*QueryResult, error) {
	params := map[string]string{
		"query": query,
	}
	if timeParam != "" {
		params["time"] = timeParam
	}

	resp, err := c.makeRequest("query", params)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}

	return &QueryResult{
		ResultType: data["resultType"].(string),
		Result:     data["result"],
	}, nil
}

// ExecuteRangeQuery executes a range PromQL query
func (c *Client) ExecuteRangeQuery(query, start, end, step string) (*QueryResult, error) {
	params := map[string]string{
		"query": query,
		"start": start,
		"end":   end,
		"step":  step,
	}

	resp, err := c.makeRequest("query_range", params)
	if err != nil {
		return nil, fmt.Errorf("failed to execute range query: %w", err)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}

	return &QueryResult{
		ResultType: data["resultType"].(string),
		Result:     data["result"],
	}, nil
}

// ListMetrics lists all available metric names
func (c *Client) ListMetrics() ([]string, error) {
	resp, err := c.makeRequest("label/__name__/values", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list metrics: %w", err)
	}

	data, ok := resp.Data.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}

	metrics := make([]string, len(data))
	for i, item := range data {
		metrics[i] = item.(string)
	}

	return metrics, nil
}

// MetricMetadata represents metadata for a metric
type MetricMetadata map[string]interface{}

// GetMetricMetadata gets metadata for a specific metric
func (c *Client) GetMetricMetadata(metric string) (MetricMetadata, error) {
	params := map[string]string{
		"metric": metric,
	}

	resp, err := c.makeRequest("metadata", params)
	if err != nil {
		return nil, fmt.Errorf("failed to get metric metadata: %w", err)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}

	// The API returns data with metric names as keys
	// For a specific metric query, we need to extract that metric's metadata
	if metricData, exists := data[metric]; exists {
		if metadata, ok := metricData.([]interface{}); ok {
			// Convert to our MetricMetadata type (which is map[string]interface{})
			result := make(MetricMetadata)
			result[metric] = metadata
			return result, nil
		}
	}

	// If no specific metric found, return the entire data (for compatibility)
	return data, nil
}

// TargetsResult represents the result of the targets API
type TargetsResult struct {
	ActiveTargets  []interface{} `json:"activeTargets"`
	DroppedTargets []interface{} `json:"droppedTargets"`
}

// GetTargets gets information about scrape targets
func (c *Client) GetTargets() (*TargetsResult, error) {
	resp, err := c.makeRequest("targets", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get targets: %w", err)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}

	result := &TargetsResult{}
	if activeTargets, exists := data["activeTargets"]; exists {
		if targets, ok := activeTargets.([]interface{}); ok {
			result.ActiveTargets = targets
		}
	}
	if droppedTargets, exists := data["droppedTargets"]; exists {
		if targets, ok := droppedTargets.([]interface{}); ok {
			result.DroppedTargets = targets
		}
	}

	return result, nil
} 