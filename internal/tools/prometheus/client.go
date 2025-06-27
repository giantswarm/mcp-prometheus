package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
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
	logger.Debug("Creating new Prometheus client", "url", config.URL, "orgID", config.OrgID)

	// Validate URL
	if config.URL == "" {
		logger.Error("Prometheus URL is empty")
		return &Client{
			client: nil,
			config: config,
			logger: logger,
		}
	}

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

	logger.Debug("Creating Prometheus API client", "address", config.URL)

	// Create the official Prometheus client
	promClient, err := api.NewClient(api.Config{
		Address:      config.URL,
		RoundTripper: roundTripper,
	})
	if err != nil {
		logger.Error("Failed to create Prometheus client", "error", err, "url", config.URL)
		// Return a client that will fail on use rather than panicking here
		return &Client{
			client: nil,
			config: config,
			logger: logger,
		}
	}

	logger.Debug("Successfully created Prometheus client", "address", config.URL)

	return &Client{
		client: v1.NewAPI(promClient),
		config: config,
		logger: logger,
	}
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

// ExecuteQueryWithOptions executes an instant PromQL query with additional options
func (c *Client) ExecuteQueryWithOptions(query string, timeParam string, options QueryOptions) (*QueryResult, error) {
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

	// Set timeout
	timeout := 30 * time.Second
	if options.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(options.Timeout); err == nil {
			timeout = parsedTimeout
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build API options
	var apiOptions []v1.Option
	if options.Limit != "" {
		if limit, err := strconv.ParseUint(options.Limit, 10, 64); err == nil {
			apiOptions = append(apiOptions, v1.WithLimit(limit))
		}
	}
	if options.Stats != "" && options.Stats == "all" {
		apiOptions = append(apiOptions, v1.WithStats(v1.AllStatsValue))
	}
	if options.LookbackDelta != "" {
		if delta, err := time.ParseDuration(options.LookbackDelta); err == nil {
			apiOptions = append(apiOptions, v1.WithLookbackDelta(delta))
		}
	}
	if timeout != 30*time.Second {
		apiOptions = append(apiOptions, v1.WithTimeout(timeout))
	}

	result, warnings, err := c.client.Query(ctx, query, queryTime, apiOptions...)
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

// ExecuteRangeQueryWithOptions executes a range PromQL query with additional options
func (c *Client) ExecuteRangeQueryWithOptions(query, start, end, step string, options QueryOptions) (*QueryResult, error) {
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

	// Set timeout
	timeout := 60 * time.Second
	if options.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(options.Timeout); err == nil {
			timeout = parsedTimeout
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	queryRange := v1.Range{
		Start: startTime,
		End:   endTime,
		Step:  time.Duration(stepDuration),
	}

	// Build API options
	var apiOptions []v1.Option
	if options.Limit != "" {
		if limit, err := strconv.ParseUint(options.Limit, 10, 64); err == nil {
			apiOptions = append(apiOptions, v1.WithLimit(limit))
		}
	}
	if options.Stats != "" && options.Stats == "all" {
		apiOptions = append(apiOptions, v1.WithStats(v1.AllStatsValue))
	}
	if options.LookbackDelta != "" {
		if delta, err := time.ParseDuration(options.LookbackDelta); err == nil {
			apiOptions = append(apiOptions, v1.WithLookbackDelta(delta))
		}
	}
	if timeout != 60*time.Second {
		apiOptions = append(apiOptions, v1.WithTimeout(timeout))
	}

	result, warnings, err := c.client.QueryRange(ctx, query, queryRange, apiOptions...)
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

// QueryOptions holds optional parameters for queries
type QueryOptions struct {
	Timeout       string
	Limit         string
	Stats         string
	LookbackDelta string
}

// ListMetrics lists all available metric names
func (c *Client) ListMetrics() ([]string, error) {
	return c.ListMetricsWithOptions(ListMetricsOptions{})
}

// ListMetricsOptions holds optional parameters for listing metrics
type ListMetricsOptions struct {
	StartTime string
	EndTime   string
	Matches   []string
}

// ListMetricsWithOptions lists all available metric names with filtering options
func (c *Client) ListMetricsWithOptions(options ListMetricsOptions) ([]string, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var startTime, endTime time.Time
	var err error

	if options.StartTime != "" {
		startTime, err = time.Parse(time.RFC3339, options.StartTime)
		if err != nil {
			return nil, fmt.Errorf("invalid start time: %w", err)
		}
	}

	if options.EndTime != "" {
		endTime, err = time.Parse(time.RFC3339, options.EndTime)
		if err != nil {
			return nil, fmt.Errorf("invalid end time: %w", err)
		}
	}

	labelValues, warnings, err := c.client.LabelValues(ctx, "__name__", options.Matches, startTime, endTime)
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
	return c.GetMetricMetadataWithOptions(metric, MetricMetadataOptions{})
}

// MetricMetadataOptions holds optional parameters for getting metric metadata
type MetricMetadataOptions struct {
	Limit string
}

// GetMetricMetadataWithOptions gets metadata for a specific metric with options
func (c *Client) GetMetricMetadataWithOptions(metric string, options MetricMetadataOptions) (MetricMetadata, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metadata, err := c.client.Metadata(ctx, metric, options.Limit)
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

// LabelNamesResult represents the result of listing label names
type LabelNamesResult struct {
	LabelNames []string `json:"labelNames"`
	Warnings   []string `json:"warnings,omitempty"`
}

// ListLabelNames gets all available label names
func (c *Client) ListLabelNames(options LabelOptions) (*LabelNamesResult, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var startTime, endTime time.Time
	var err error

	if options.StartTime != "" {
		startTime, err = time.Parse(time.RFC3339, options.StartTime)
		if err != nil {
			return nil, fmt.Errorf("invalid start time: %w", err)
		}
	}

	if options.EndTime != "" {
		endTime, err = time.Parse(time.RFC3339, options.EndTime)
		if err != nil {
			return nil, fmt.Errorf("invalid end time: %w", err)
		}
	}

	// Build API options
	var apiOptions []v1.Option
	if options.Limit != "" {
		if limit, err := strconv.ParseUint(options.Limit, 10, 64); err == nil {
			apiOptions = append(apiOptions, v1.WithLimit(limit))
		}
	}

	labelNames, warnings, err := c.client.LabelNames(ctx, options.Matches, startTime, endTime, apiOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to list label names: %w", err)
	}

	// Convert warnings to string slice
	warningStrs := make([]string, len(warnings))
	for i, w := range warnings {
		warningStrs[i] = string(w)
	}

	return &LabelNamesResult{
		LabelNames: labelNames,
		Warnings:   warningStrs,
	}, nil
}

// LabelValuesResult represents the result of listing label values
type LabelValuesResult struct {
	LabelValues []string `json:"labelValues"`
	Warnings    []string `json:"warnings,omitempty"`
}

// LabelOptions holds options for label-related queries
type LabelOptions struct {
	StartTime string
	EndTime   string
	Matches   []string
	Limit     string
}

// ListLabelValues gets values for a specific label
func (c *Client) ListLabelValues(label string, options LabelOptions) (*LabelValuesResult, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var startTime, endTime time.Time
	var err error

	if options.StartTime != "" {
		startTime, err = time.Parse(time.RFC3339, options.StartTime)
		if err != nil {
			return nil, fmt.Errorf("invalid start time: %w", err)
		}
	}

	if options.EndTime != "" {
		endTime, err = time.Parse(time.RFC3339, options.EndTime)
		if err != nil {
			return nil, fmt.Errorf("invalid end time: %w", err)
		}
	}

	// Build API options
	var apiOptions []v1.Option
	if options.Limit != "" {
		if limit, err := strconv.ParseUint(options.Limit, 10, 64); err == nil {
			apiOptions = append(apiOptions, v1.WithLimit(limit))
		}
	}

	labelValues, warnings, err := c.client.LabelValues(ctx, label, options.Matches, startTime, endTime, apiOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to list label values: %w", err)
	}

	// Convert to string slice
	values := make([]string, len(labelValues))
	for i, v := range labelValues {
		values[i] = string(v)
	}

	// Convert warnings to string slice
	warningStrs := make([]string, len(warnings))
	for i, w := range warnings {
		warningStrs[i] = string(w)
	}

	return &LabelValuesResult{
		LabelValues: values,
		Warnings:    warningStrs,
	}, nil
}

// SeriesResult represents the result of finding series
type SeriesResult struct {
	Series   []map[string]string `json:"series"`
	Warnings []string            `json:"warnings,omitempty"`
}

// SeriesOptions holds options for series queries
type SeriesOptions struct {
	StartTime string
	EndTime   string
	Limit     string
}

// FindSeries finds series by label matchers
func (c *Client) FindSeries(matches []string, options SeriesOptions) (*SeriesResult, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var startTime, endTime time.Time
	var err error

	if options.StartTime != "" {
		startTime, err = time.Parse(time.RFC3339, options.StartTime)
		if err != nil {
			return nil, fmt.Errorf("invalid start time: %w", err)
		}
	}

	if options.EndTime != "" {
		endTime, err = time.Parse(time.RFC3339, options.EndTime)
		if err != nil {
			return nil, fmt.Errorf("invalid end time: %w", err)
		}
	}

	// Build API options
	var apiOptions []v1.Option
	if options.Limit != "" {
		if limit, err := strconv.ParseUint(options.Limit, 10, 64); err == nil {
			apiOptions = append(apiOptions, v1.WithLimit(limit))
		}
	}

	series, warnings, err := c.client.Series(ctx, matches, startTime, endTime, apiOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to find series: %w", err)
	}

	// Convert to our format
	result := make([]map[string]string, len(series))
	for i, s := range series {
		result[i] = make(map[string]string)
		for k, v := range s {
			result[i][string(k)] = string(v)
		}
	}

	// Convert warnings to string slice
	warningStrs := make([]string, len(warnings))
	for i, w := range warnings {
		warningStrs[i] = string(w)
	}

	return &SeriesResult{
		Series:   result,
		Warnings: warningStrs,
	}, nil
}

// GetRules gets recording and alerting rules
func (c *Client) GetRules() (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rules, err := c.client.Rules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get rules: %w", err)
	}

	return rules, nil
}

// GetAlerts gets active alerts
func (c *Client) GetAlerts() (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	alerts, err := c.client.Alerts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get alerts: %w", err)
	}

	return alerts, nil
}

// GetAlertManagers gets AlertManager discovery info
func (c *Client) GetAlertManagers() (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	alertManagers, err := c.client.AlertManagers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get alert managers: %w", err)
	}

	return alertManagers, nil
}

// GetConfig gets Prometheus configuration
func (c *Client) GetConfig() (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config, err := c.client.Config(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}

	return config, nil
}

// GetFlags gets runtime flags
func (c *Client) GetFlags() (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	flags, err := c.client.Flags(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get flags: %w", err)
	}

	return flags, nil
}

// GetBuildInfo gets build information
func (c *Client) GetBuildInfo() (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	buildInfo, err := c.client.Buildinfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get build info: %w", err)
	}

	return buildInfo, nil
}

// GetRuntimeInfo gets runtime information
func (c *Client) GetRuntimeInfo() (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runtimeInfo, err := c.client.Runtimeinfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get runtime info: %w", err)
	}

	return runtimeInfo, nil
}

// GetTSDBStats gets TSDB cardinality statistics
func (c *Client) GetTSDBStats(options TSDBOptions) (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build API options
	var apiOptions []v1.Option
	if options.Limit != "" {
		if limit, err := strconv.ParseUint(options.Limit, 10, 64); err == nil {
			apiOptions = append(apiOptions, v1.WithLimit(limit))
		}
	}

	tsdbStats, err := c.client.TSDB(ctx, apiOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to get TSDB stats: %w", err)
	}

	return tsdbStats, nil
}

// TSDBOptions holds options for TSDB queries
type TSDBOptions struct {
	Limit string
}

// QueryExemplars queries exemplars for traces
func (c *Client) QueryExemplars(query, start, end string) (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

	exemplars, err := c.client.QueryExemplars(ctx, query, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query exemplars: %w", err)
	}

	return exemplars, nil
}

// GetTargetsMetadata gets metadata about metrics from specific targets
func (c *Client) GetTargetsMetadata(matchTarget, metric, limit string) (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("Prometheus client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	targetsMetadata, err := c.client.TargetsMetadata(ctx, matchTarget, metric, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get targets metadata: %w", err)
	}

	return targetsMetadata, nil
}
