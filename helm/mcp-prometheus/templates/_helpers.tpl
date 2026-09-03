{{/*
Expand the name of the chart.
*/}}
{{- define "mcp-prometheus.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "mcp-prometheus.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "mcp-prometheus.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" | trimSuffix "_" }}
{{- end }}

{{/*
Common labels including team annotation
*/}}
{{- define "mcp-prometheus.labels" -}}
helm.sh/chart: {{ include "mcp-prometheus.chart" . }}
{{ include "mcp-prometheus.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
application.giantswarm.io/team: {{ index .Chart.Annotations "io.giantswarm.application.team" | quote }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "mcp-prometheus.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mcp-prometheus.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "mcp-prometheus.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "mcp-prometheus.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the image path
*/}}
{{- define "mcp-prometheus.image" -}}
{{- printf "%s/%s:%s" .Values.image.registry .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) }}
{{- end }}

{{/*
OAuth identity provider (app.oauth.provider), defaulting to dex and rejecting
anything the binary does not implement so a typo fails at render time.
*/}}
{{- define "mcp-prometheus.oauthProvider" -}}
{{- $p := .Values.app.oauth.provider | default "dex" -}}
{{- if not (has $p (list "dex" "google")) -}}
{{- fail (printf "app.oauth.provider must be one of: dex, google (got %q)" $p) -}}
{{- end -}}
{{- $p -}}
{{- end }}

{{/*
Effective tenancy mode (app.tenancy.mode), defaulting to grafana-organization.
*/}}
{{- define "mcp-prometheus.tenancyMode" -}}
{{- .Values.app.tenancy.mode | default "grafana-organization" -}}
{{- end }}
