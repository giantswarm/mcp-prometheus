# mcp-prometheus

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for mcp-prometheus - MCP server for Prometheus metrics

**Homepage:** <https://github.com/giantswarm/mcp-prometheus>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Giant Swarm | <team-planeteers@giantswarm.io> |  |

## Source Code

* <https://github.com/giantswarm/mcp-prometheus>

## Requirements

Kubernetes: `>=1.25.0-0`

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| replicaCount | int | `1` |  |
| image.registry | string | `"gsoci.azurecr.io"` |  |
| image.repository | string | `"giantswarm/mcp-prometheus"` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.tag | string | `""` |  |
| imagePullSecrets | list | `[]` |  |
| nameOverride | string | `""` |  |
| fullnameOverride | string | `""` |  |
| serviceAccount.create | bool | `true` |  |
| serviceAccount.automount | bool | `true` |  |
| serviceAccount.annotations | object | `{}` |  |
| serviceAccount.name | string | `""` |  |
| podAnnotations | object | `{}` |  |
| podLabels | object | `{}` |  |
| podSecurityContext.runAsUser | int | `1000` |  |
| podSecurityContext.runAsGroup | int | `1000` |  |
| podSecurityContext.runAsNonRoot | bool | `true` |  |
| podSecurityContext.fsGroup | int | `1000` |  |
| securityContext.readOnlyRootFilesystem | bool | `true` |  |
| securityContext.allowPrivilegeEscalation | bool | `false` |  |
| securityContext.runAsUser | int | `1000` |  |
| securityContext.runAsGroup | int | `1000` |  |
| securityContext.runAsNonRoot | bool | `true` |  |
| securityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| service.type | string | `"ClusterIP"` |  |
| service.port | int | `8080` |  |
| service.appProtocol | string | `""` |  |
| ingress.enabled | bool | `false` |  |
| ingress.className | string | `""` |  |
| ingress.annotations | object | `{}` |  |
| ingress.hosts[0].host | string | `"mcp-prometheus.local"` |  |
| ingress.hosts[0].paths[0].path | string | `"/"` |  |
| ingress.hosts[0].paths[0].pathType | string | `"Prefix"` |  |
| ingress.tls | list | `[]` |  |
| resources.limits.cpu | string | `"500m"` |  |
| resources.limits.memory | string | `"512Mi"` |  |
| resources.requests.cpu | string | `"100m"` |  |
| resources.requests.memory | string | `"128Mi"` |  |
| livenessProbe | object | `{}` |  |
| readinessProbe | object | `{}` |  |
| autoscaling.enabled | bool | `false` |  |
| autoscaling.minReplicas | int | `1` |  |
| autoscaling.maxReplicas | int | `100` |  |
| autoscaling.targetCPUUtilizationPercentage | int | `80` |  |
| volumes[0].name | string | `"tmp"` |  |
| volumes[0].emptyDir | object | `{}` |  |
| volumeMounts[0].name | string | `"tmp"` |  |
| volumeMounts[0].mountPath | string | `"/tmp"` |  |
| ciliumNetworkPolicy.enabled | bool | `true` |  |
| ciliumNetworkPolicy.labels | object | `{}` |  |
| ciliumNetworkPolicy.annotations | object | `{}` |  |
| monitoring.enabled | bool | `true` |  |
| monitoring.serviceMonitor.enabled | bool | `false` |  |
| monitoring.serviceMonitor.labels | object | `{}` |  |
| nodeSelector | object | `{}` |  |
| tolerations | list | `[]` |  |
| affinity | object | `{}` |  |
| app.server.transport | string | `"streamable-http"` |  |
| app.server.httpAddr | string | `":8080"` |  |
| app.server.sseEndpoint | string | `"/sse"` |  |
| app.server.messageEndpoint | string | `"/message"` |  |
| app.server.httpEndpoint | string | `"/mcp"` |  |
| app.server.debug | bool | `false` |  |
| app.server.metricsAddr | string | `":9091"` |  |
| app.oauth.enabled | bool | `false` |  |
| app.oauth.provider | string | `"dex"` |  |
| app.oauth.redirectURL | string | `""` |  |
| app.oauth.google.clientID | string | `""` |  |
| app.oauth.existingSecret | string | `""` |  |
| app.oauth.dexClientSecret | string | `""` |  |
| app.oauth.googleClientSecret | string | `""` |  |
| app.oauth.encryptionKey | string | `""` |  |
| app.oauth.allowPublicRegistration | bool | `false` |  |
| app.oauth.allowPrivateURLs | bool | `false` |  |
| app.oauth.dexCASecret.name | string | `""` |  |
| app.oauth.dexCASecret.key | string | `"ca.crt"` |  |
| app.oauth.storage.type | string | `"memory"` |  |
| app.oauth.storage.valkey.url | string | `""` |  |
| app.oauth.storage.valkey.password | string | `""` |  |
| app.oauth.storage.valkey.tlsEnabled | bool | `false` |  |
| app.oauth.storage.valkey.keyPrefix | string | `"mcp-prometheus:"` |  |
| app.oauth.trustedAudiences | list | `[]` |  |
| app.tenancy.mode | string | `"grafana-organization"` |  |
| app.tenancy.static.tenants | string | `""` |  |
| app.tenancy.static.groups | object | `{}` |  |
| app.env | list | `[]` |  |
| gatewayAPI.enabled | bool | `false` |  |
| gatewayAPI.httpRoute.parentRefs | list | `[]` |  |
| gatewayAPI.httpRoute.hostnames | list | `[]` |  |
| gatewayAPI.httpRoute.labels | object | `{}` |  |
| gatewayAPI.httpRoute.annotations | object | `{}` |  |
| gatewayAPI.httpRoute.rules | list | `[]` |  |
| gatewayAPI.backendTrafficPolicy.enabled | bool | `false` |  |
| gatewayAPI.backendTrafficPolicy.timeout | string | `"0s"` |  |
| gatewayAPI.backendTrafficPolicy.annotations | object | `{}` |  |
| gatewayAPI.backendTrafficPolicy.labels | object | `{}` |  |
