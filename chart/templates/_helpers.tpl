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
