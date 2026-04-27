{{/*
Standard helper templates for the datawatch chart. Names follow the
helm-create scaffold so external tooling (kustomize overlays, ArgoCD
ApplicationSet, etc.) finds the conventional labels.
*/}}

{{- define "datawatch.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "datawatch.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "datawatch.serviceAccountName" -}}
{{- if .Values.rbac.serviceAccountName -}}
{{- .Values.rbac.serviceAccountName -}}
{{- else -}}
{{- printf "%s-datawatch" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "datawatch.labels" -}}
app.kubernetes.io/name: {{ include "datawatch.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "datawatch.selectorLabels" -}}
app.kubernetes.io/name: {{ include "datawatch.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Image reference: registry/repository:tag.
     v5.26.6 — strip a leading `v` from the tag because GHCR (and
     the CI workflow at .github/workflows/containers.yaml) publishes
     tags WITHOUT the `v` prefix (e.g. `5.26.5`, not `v5.26.5`).
     Operators commonly paste a release tag including `v`; this lets
     either form work without surprising ImagePullBackOff. */}}
{{- define "datawatch.image" -}}
{{- $rawTag := default .Chart.AppVersion .Values.image.tag -}}
{{- $tag := trimPrefix "v" $rawTag -}}
{{- if .Values.image.registry -}}
{{ .Values.image.registry }}/{{ .Values.image.repository }}:{{ $tag }}
{{- else -}}
{{ .Values.image.repository }}:{{ $tag }}
{{- end -}}
{{- end -}}
