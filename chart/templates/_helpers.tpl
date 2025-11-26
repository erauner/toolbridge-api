{{/*
Expand the name of the chart.
*/}}
{{- define "toolbridge-api.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "toolbridge-api.fullname" -}}
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
{{- define "toolbridge-api.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "toolbridge-api.labels" -}}
helm.sh/chart: {{ include "toolbridge-api.chart" . }}
{{ include "toolbridge-api.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "toolbridge-api.selectorLabels" -}}
app.kubernetes.io/name: {{ include "toolbridge-api.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: api
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "toolbridge-api.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "toolbridge-api.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Database service name
*/}}
{{- define "toolbridge-api.database.serviceName" -}}
{{- printf "%s-postgres-rw" (include "toolbridge-api.fullname" .) }}
{{- end }}

{{/*
Secret name
*/}}
{{- define "toolbridge-api.secretName" -}}
{{- if .Values.secrets.existingSecret }}
{{- .Values.secrets.existingSecret }}
{{- else }}
{{- printf "%s-secret" (include "toolbridge-api.fullname" .) }}
{{- end }}
{{- end }}

{{/*
OAuth Client Helpers
*/}}

{{/*
Get primary JWT audience
Prefers: 1) api.jwt.audience (if set), 2) oauthClients.userClient.clientId, 3) empty string
*/}}
{{- define "toolbridge-api.jwt.audience" -}}
{{- if .Values.api.jwt.audience }}
{{- .Values.api.jwt.audience }}
{{- else if .Values.oauthClients.userClient.clientId }}
{{- .Values.oauthClients.userClient.clientId }}
{{- end }}
{{- end }}

{{/*
Get MCP OAuth audience
Prefers: 1) api.mcp.oauthAudience (if set), 2) oauthClients.mcpClient.clientId (if enabled), 3) empty string
*/}}
{{- define "toolbridge-api.mcp.audience" -}}
{{- if .Values.api.mcp.oauthAudience }}
{{- .Values.api.mcp.oauthAudience }}
{{- else if and .Values.oauthClients.mcpClient.enabled .Values.oauthClients.mcpClient.clientId }}
{{- .Values.oauthClients.mcpClient.clientId }}
{{- end }}
{{- end }}

{{/*
Validate OAuth client configuration
Fails the deployment if configuration is invalid
*/}}
{{- define "toolbridge-api.validateOAuthClients" -}}
{{- if .Values.api.jwt.issuer }}
  {{- $hasUserClient := or .Values.api.jwt.audience .Values.oauthClients.userClient.clientId }}
  {{- if not $hasUserClient }}
    {{- fail "When jwt.issuer is configured, you must set either api.jwt.audience OR oauthClients.userClient.clientId" }}
  {{- end }}
  {{- if and .Values.api.mcp.enabled (not (or .Values.api.mcp.oauthAudience .Values.oauthClients.mcpClient.clientId)) }}
    {{- if .Values.oauthClients.mcpClient.enabled }}
      {{- fail "When MCP is enabled and oauthClients.mcpClient.enabled=true, you must set oauthClients.mcpClient.clientId" }}
    {{- end }}
  {{- end }}
{{- end }}
{{- end }}
