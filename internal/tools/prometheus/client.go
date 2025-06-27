package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/giantswarm/mcp-prometheus/internal/server"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// orgIDRoundTripper adds Organization ID header to requests for multi-tenant setups
type orgIDRoundTripper struct {
	orgID string
	rt    http.RoundTripper
}

func (o *orgIDRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if o.orgID != "" {
		req.Header.Set("X-Scope-OrgID", o.orgID)
	}
	return o.rt.RoundTrip(req)
}

// basicAuthRoundTripper adds basic authentication to requests
type basicAuthRoundTripper struct {
	username string
	password string
	rt       http.RoundTripper
}

func (b *basicAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(b.username, b.password)
	return b.rt.RoundTrip(req)
}

// bearerTokenRoundTripper adds bearer token authentication to requests
type bearerTokenRoundTripper struct {
	token string
	rt    http.RoundTripper
}

func (b *bearerTokenRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+b.token)
	return b.rt.RoundTrip(req)
}

// Client wraps the official Prometheus client with logging
type Client struct {
	client v1.API
	config server.PrometheusConfig
	logger server.Logger
}

// NewClient creates a new Prometheus client using the official client library
func NewClient(config server.PrometheusConfig, logger server.Logger) *Client {
	// Start with default transport
	roundTripper := http.DefaultTransport

	// Add authentication layer
	if config.Token != "" {
		// Bearer token authentication
		roundTripper = &bearerTokenRoundTripper{
			token: config.Token,
			rt:    roundTripper,
		}
		logger.Debug("Using bearer token authentication")
	} else if config.Username != "" && config.Password != "" {
		// Basic authentication
		roundTripper = &basicAuthRoundTripper{
			username: config.Username,
			password: config.Password,
			rt:       roundTripper,
		}
		logger.Debug("Using basic authentication", "username", config.Username)
	} else {
		logger.Debug("No authentication configured")
	}

	// Add organization ID layer if specified
	if config.OrgID != "" {
		roundTripper = &orgIDRoundTripper{
			orgID: config.OrgID,
			rt:    roundTripper,
		}
		logger.Debug("Using organization ID", "orgID", config.OrgID)
	}

	// Create the official Prometheus client
	promClient, err := api.NewClient(api.Config{
		Address:      config.URL,
		RoundTripper: roundTripper,
	})
	if err != nil {
		logger.Error("Failed to create Prometheus client", "error", err)
		// Return a client that will fail on use rather than panicking here
		return &Client{
			client: nil,
			config: config,
			logger: logger,
		}
	}

	return &Client{
		client: v1.NewAPI(promClient),
		config: config,
		logger: logger,
	}
}

// NewClientFromParams creates a new Prometheus client from individual parameters
// This function uses environment variables as defaults and validates the configuration
func NewClientFromParams(prometheusURL, orgID string, baseConfig server.PrometheusConfig, logger server.Logger) (*Client, error) {
	config, err := buildPrometheusConfig(baseConfig, prometheusURL, orgID)
	if err != nil {
		return nil, err
	}

	client := NewClient(config, logger)
	if client.client == nil {
		return nil, fmt.Errorf("failed to initialize Prometheus client")
	}

	return client, nil
}

// buildPrometheusConfig creates a PrometheusConfig based on tool parameters and environment variables
// Environment variables take precedence and cannot be overridden
func buildPrometheusConfig(baseConfig server.PrometheusConfig, prometheusURL, orgID string) (server.PrometheusConfig, error) {
	config := server.PrometheusConfig{
		Username: baseConfig.Username,
		Password: baseConfig.Password,
		Token:    baseConfig.Token,
	}

	// Handle Prometheus URL
	envURL := os.Getenv("PROMETHEUS_URL")
	if envURL != "" {
		// Environment variable takes precedence
		config.URL = envURL
	} else if prometheusURL != "" {
		// Use parameter if no environment variable is set
		config.URL = prometheusURL
	} else {
		// Neither environment variable nor parameter provided
		return config, fmt.Errorf("prometheus URL is required: either set PROMETHEUS_URL environment variable or provide prometheus_url parameter")
	}

	// Handle OrgID
	envOrgID := os.Getenv("PROMETHEUS_ORGID")
	if envOrgID != "" {
		// Environment variable takes precedence
		config.OrgID = envOrgID
	} else if orgID != "" {
		// Use parameter if no environment variable is set
		config.OrgID = orgID
	}
	// If neither is set, OrgID remains empty (which is acceptable)

	return config, nil
}

// QueryResult represents the result of an instant query
type QueryResult struct {
	ResultType string      `json:"resultType"`
	Result     interface{} `json:"result"`
}

// ExecuteQuery executes an instant PromQL query
func (c *Client) ExecuteQuery(query string, timeParam string) (*QueryResult, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	var queryTime time.Time
	var err error

	if timeParam != "" {
		// Parse the time parameter
		queryTime, err = time.Parse(time.RFC3339, timeParam)
		if err != nil {
			// Try parsing as Unix timestamp
			queryTime = time.Unix(0, 0)
			if _, parseErr := fmt.Sscanf(timeParam, "%d", &queryTime); parseErr != nil {
				return nil, fmt.Errorf("invalid time parameter: %w", err)
			}
		}
	} else {
		queryTime = time.Now()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, warnings, err := c.client.Query(ctx, query, queryTime)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	if len(warnings) > 0 {
		c.logger.Warn("Query returned warnings", "warnings", warnings)
	}

	return &QueryResult{
		ResultType: result.Type().String(),
		Result:     result,
	}, nil
}

// ExecuteRangeQuery executes a range PromQL query
func (c *Client) ExecuteRangeQuery(query, start, end, step string) (*QueryResult, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	// Parse start time
	startTime, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return nil, fmt.Errorf("invalid start time: %w", err)
	}

	// Parse end time
	endTime, err := time.Parse(time.RFC3339, end)
	if err != nil {
		return nil, fmt.Errorf("invalid end time: %w", err)
	}

	// Parse step duration
	stepDuration, err := model.ParseDuration(step)
	if err != nil {
		return nil, fmt.Errorf("invalid step duration: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	queryRange := v1.Range{
		Start: startTime,
		End:   endTime,
		Step:  time.Duration(stepDuration),
	}

	result, warnings, err := c.client.QueryRange(ctx, query, queryRange)
	if err != nil {
		return nil, fmt.Errorf("failed to execute range query: %w", err)
	}

	if len(warnings) > 0 {
		c.logger.Warn("Range query returned warnings", "warnings", warnings)
	}

	return &QueryResult{
		ResultType: result.Type().String(),
		Result:     result,
	}, nil
}

// ListMetrics lists all available metric names
func (c *Client) ListMetrics() ([]string, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	labelValues, warnings, err := c.client.LabelValues(ctx, "__name__", nil, time.Time{}, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("failed to list metrics: %w", err)
	}

	if len(warnings) > 0 {
		c.logger.Warn("List metrics returned warnings", "warnings", warnings)
	}

	// Convert model.LabelValues to []string
	metrics := make([]string, len(labelValues))
	for i, labelValue := range labelValues {
		metrics[i] = string(labelValue)
	}

	return metrics, nil
}

// MetricMetadata represents metadata for a metric
type MetricMetadata map[string]interface{}

// GetMetricMetadata gets metadata for a specific metric
func (c *Client) GetMetricMetadata(metric string) (MetricMetadata, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metadata, err := c.client.Metadata(ctx, metric, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get metric metadata: %w", err)
	}

	// Convert to our MetricMetadata format
	result := make(MetricMetadata)

	// The official client returns map[string][]v1.Metadata
	// We need to convert this to match our expected format
	for metricName, metadataList := range metadata {
		// Convert []v1.Metadata to []interface{}
		convertedList := make([]interface{}, len(metadataList))
		for i, md := range metadataList {
			convertedList[i] = map[string]interface{}{
				"type": md.Type,
				"help": md.Help,
				"unit": md.Unit,
			}
		}
		result[metricName] = convertedList
	}

	return result, nil
}

// TargetsResult represents the result of the targets API
type TargetsResult struct {
	ActiveTargets  []interface{} `json:"activeTargets"`
	DroppedTargets []interface{} `json:"droppedTargets"`
}

// GetTargets gets information about scrape targets
func (c *Client) GetTargets() (*TargetsResult, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	targets, err := c.client.Targets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get targets: %w", err)
	}

	// Convert v1.TargetsResult to our TargetsResult format
	result := &TargetsResult{
		ActiveTargets:  make([]interface{}, len(targets.Active)),
		DroppedTargets: make([]interface{}, len(targets.Dropped)),
	}

	// Convert active targets
	for i, target := range targets.Active {
		result.ActiveTargets[i] = map[string]interface{}{
			"discoveredLabels":   target.DiscoveredLabels,
			"labels":             target.Labels,
			"scrapePool":         target.ScrapePool,
			"scrapeUrl":          target.ScrapeURL,
			"globalUrl":          target.GlobalURL,
			"lastError":          target.LastError,
			"lastScrape":         target.LastScrape,
			"lastScrapeDuration": target.LastScrapeDuration,
			"health":             target.Health,
		}
	}

	// Convert dropped targets
	for i, target := range targets.Dropped {
		result.DroppedTargets[i] = map[string]interface{}{
			"discoveredLabels": target.DiscoveredLabels,
		}
	}

	return result, nil
}
