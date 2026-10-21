# Tools

Generated from the registered tools by `TestToolsDocIsCurrent`; regenerate with `make tools-doc`.

| Tool | Kind |
|---|---|
| `check_ready` | read-only |
| `execute_query` | read-only |
| `execute_range_query` | read-only |
| `find_series` | read-only |
| `get_alertmanager_alerts` | read-only |
| `get_alertmanagers` | read-only |
| `get_alerts` | read-only |
| `get_build_info` | read-only |
| `get_config` | read-only |
| `get_flags` | read-only |
| `get_metric_metadata` | read-only |
| `get_rules` | read-only |
| `get_runtime_info` | read-only |
| `get_targets` | read-only |
| `get_targets_metadata` | read-only |
| `get_tsdb_stats` | read-only |
| `list_label_names` | read-only |
| `list_label_values` | read-only |
| `query_exemplars` | read-only |

## `check_ready`

```text
Check the readiness of the Prometheus or Mimir server: whether it is ready to serve traffic.

Use it when other tools fail to reach the server.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `execute_query`

```text
Run a PromQL instant query: the value of a metric or expression at one point in time.

For values over a time window use execute_range_query; for metric names use list_label_values with label __name__, for label names list_label_names.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "limit": {
      "description": "Maximum entries returned",
      "type": "string"
    },
    "lookback_delta": {
      "description": "Query lookback delta (e.g., '5m')",
      "type": "string"
    },
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    },
    "query": {
      "description": "PromQL query string",
      "type": "string"
    },
    "stats": {
      "description": "Include query statistics: 'all'",
      "type": "string"
    },
    "time": {
      "description": "RFC3339 or Unix timestamp; default now",
      "type": "string"
    },
    "timeout": {
      "description": "Query timeout (e.g., '30s', '1m', '5m')",
      "type": "string"
    },
    "unlimited": {
      "description": "'true' lifts the 50000-byte output cap: the result may be huge",
      "type": "string"
    }
  },
  "required": [
    "query"
  ],
  "type": "object"
}
```

## `execute_range_query`

```text
Run a PromQL range query: the values of a metric or expression over a time window, one sample per step.

For a single point in time use execute_query.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "end": {
      "description": "End time (RFC3339)",
      "type": "string"
    },
    "limit": {
      "description": "Maximum entries returned",
      "type": "string"
    },
    "lookback_delta": {
      "description": "Query lookback delta (e.g., '5m')",
      "type": "string"
    },
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    },
    "query": {
      "description": "PromQL query string",
      "type": "string"
    },
    "start": {
      "description": "Start time (RFC3339)",
      "type": "string"
    },
    "stats": {
      "description": "Include query statistics: 'all'",
      "type": "string"
    },
    "step": {
      "description": "Query resolution step width (e.g., '15s', '1m', '1h')",
      "type": "string"
    },
    "timeout": {
      "description": "Query timeout (e.g., '30s', '1m', '5m')",
      "type": "string"
    },
    "unlimited": {
      "description": "'true' lifts the 50000-byte output cap: the result may be huge",
      "type": "string"
    }
  },
  "required": [
    "query",
    "start",
    "end",
    "step"
  ],
  "type": "object"
}
```

## `find_series`

```text
Find time series by label matcher: the label sets of the matching series.

Lists at most 50 series, even with a higher limit; narrow with matches. Returns labels, not samples: for values use execute_query.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "end_time": {
      "description": "End time (RFC3339)",
      "type": "string"
    },
    "limit": {
      "description": "Maximum entries returned",
      "type": "string"
    },
    "matches": {
      "description": "Array of label matchers (e.g., ['{job=\"prometheus\"}', '{__name__=~\"http_.*\"}'])",
      "type": "array"
    },
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    },
    "start_time": {
      "description": "Start time (RFC3339)",
      "type": "string"
    }
  },
  "required": [
    "matches"
  ],
  "type": "object"
}
```

## `get_alertmanager_alerts`

```text
List the alerts that notify from Alertmanager: active, not silenced or inhibited, with labels and receivers.

For firing and pending alerts at the ruler use get_alerts. An answer over 50000 bytes keeps the oldest complete alerts and a truncated marker with the omitted count.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "alertmanager_url": {
      "description": "Alertmanager base URL (e.g. 'http://mimir-gateway.mimir/alertmanager' or 'http://alertmanager:9093'); defaults to ALERTMANAGER_URL",
      "type": "string"
    },
    "filter": {
      "description": "Only alerts matching every label matcher, e.g. ['team=\"bumblebee\"', 'severity=~\"page|notify\"']",
      "items": {
        "type": "string"
      },
      "type": "array"
    },
    "limit": {
      "description": "Maximum alerts returned, oldest first",
      "minimum": 1,
      "type": "number"
    },
    "org_id": {
      "description": "Mimir Alertmanager tenant, 'a|b' for several; under OAuth only your alerting tenants, all of them when omitted",
      "type": "string"
    },
    "receiver": {
      "description": "Only alerts routed to a receiver matching this regex",
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `get_alertmanagers`

```text
List the Alertmanager instances Prometheus discovered to send alerts to, active and dropped.

For the alerts in Alertmanager use get_alertmanager_alerts. Prometheus only.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `get_alerts`

```text
List the firing and pending alerts of the Prometheus or Mimir ruler.

For the alerts that notify, not silenced or inhibited, use get_alertmanager_alerts. On Mimir it reads one tenant per call: pass org_id if no default tenant is set.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "org_id": {
      "description": "Mimir tenant, one per call",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `get_build_info`

```text
Get the Prometheus server's build information: version, revision, branch and Go version.

For runtime state use get_runtime_info.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `get_config`

```text
Get the loaded Prometheus configuration file: scrape configs, rule files, remote write.

For the live state of the scrape targets use get_targets. Prometheus only.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `get_flags`

```text
Get the command-line flags the Prometheus server was started with.

For the scrape and rule configuration use get_config. Prometheus only.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `get_metric_metadata`

```text
Get a metric's metadata: its type, help text and unit.

For the metadata as each scrape target exposes it use get_targets_metadata.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "limit": {
      "description": "Maximum entries returned",
      "type": "string"
    },
    "metric": {
      "description": "Metric name",
      "type": "string"
    },
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    }
  },
  "required": [
    "metric"
  ],
  "type": "object"
}
```

## `get_rules`

```text
List the alerting and recording rules of the Prometheus or Mimir ruler, filtered server-side.

Prefer the filters and exclude_alerts over a full dump. On Mimir it reads one tenant per call: pass org_id if no default tenant is set.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "exclude_alerts": {
      "description": "Set to true to omit the active alerts embedded in alerting rules (much smaller output)",
      "type": "boolean"
    },
    "file": {
      "description": "Only return rule groups loaded from these files; on Mimir the file is the rule namespace",
      "items": {
        "type": "string"
      },
      "type": "array"
    },
    "org_id": {
      "description": "Mimir tenant, one per call",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    },
    "rule_group": {
      "description": "Only return rule groups with these exact names",
      "items": {
        "type": "string"
      },
      "type": "array"
    },
    "rule_name": {
      "description": "Only return rules with these exact names (e.g. ['MCPKubernetesDown'])",
      "items": {
        "type": "string"
      },
      "type": "array"
    },
    "type": {
      "description": "Return only alerting rules ('alert') or recording rules ('record'); omit for both",
      "enum": [
        "alert",
        "record"
      ],
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `get_runtime_info`

```text
Get the Prometheus server's runtime information: start time, storage retention, config reload status.

For the version use get_build_info. Prometheus only.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `get_targets`

```text
List Prometheus's scrape targets (not on Mimir) with their health, last scrape and last error, active and dropped.

For the metrics a target exposes use get_targets_metadata. Prometheus only.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `get_targets_metadata`

```text
Get metric metadata per scrape target, Prometheus only: type, help text and unit as each target exposes them.

For one metric's metadata across targets use get_metric_metadata. Prometheus only.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "limit": {
      "description": "Maximum entries returned",
      "type": "string"
    },
    "match_target": {
      "description": "Label selector of the targets",
      "type": "string"
    },
    "metric": {
      "description": "Metric name",
      "type": "string"
    },
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `get_tsdb_stats`

```text
Get TSDB cardinality statistics, Prometheus only: series count and the metrics and labels with the most series.

For the series of one metric use find_series. Prometheus only.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "limit": {
      "description": "Maximum entries returned",
      "type": "string"
    },
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `list_label_names`

```text
List the label names of the stored series, optionally of those matching a selector.

For one label's values use list_label_values.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "end_time": {
      "description": "End time (RFC3339)",
      "type": "string"
    },
    "limit": {
      "description": "Maximum entries returned",
      "type": "string"
    },
    "matches": {
      "description": "Series selectors to filter by",
      "type": "array"
    },
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    },
    "start_time": {
      "description": "Start time (RFC3339)",
      "type": "string"
    }
  },
  "required": [],
  "type": "object"
}
```

## `list_label_values`

```text
List the values of one label; the label __name__ lists the metric names.

Lists at most 100 values, even with a higher limit; narrow with matches. For the full label sets of matching series use find_series.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "end_time": {
      "description": "End time (RFC3339)",
      "type": "string"
    },
    "label": {
      "description": "Label name",
      "type": "string"
    },
    "limit": {
      "description": "Maximum entries returned",
      "type": "string"
    },
    "matches": {
      "description": "Series selectors to filter by",
      "type": "array"
    },
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    },
    "start_time": {
      "description": "Start time (RFC3339)",
      "type": "string"
    }
  },
  "required": [
    "label"
  ],
  "type": "object"
}
```

## `query_exemplars`

```text
Query exemplars for a PromQL selector: the trace IDs attached to its samples in a time window.

For the metric values use execute_range_query.
```

```json
{
  "additionalProperties": false,
  "properties": {
    "end": {
      "description": "End time (RFC3339)",
      "type": "string"
    },
    "org_id": {
      "description": "Mimir tenant, 'a|b' for several",
      "type": "string"
    },
    "prometheus_url": {
      "description": "URL if not the configured one; Mimir's ends in /prometheus",
      "type": "string"
    },
    "query": {
      "description": "PromQL selector",
      "type": "string"
    },
    "start": {
      "description": "Start time (RFC3339)",
      "type": "string"
    }
  },
  "required": [
    "query",
    "start",
    "end"
  ],
  "type": "object"
}
```
