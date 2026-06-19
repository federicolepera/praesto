{{- define "praesto.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "praesto.fullname" -}}
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

{{- define "praesto.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride -}}
{{- end -}}

{{- define "praesto.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "praesto.labels" -}}
helm.sh/chart: {{ include "praesto.chart" . }}
app.kubernetes.io/name: {{ include "praesto.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "praesto.selectorLabels" -}}
app.kubernetes.io/name: {{ include "praesto.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end -}}

{{- define "praesto.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (printf "%s-controller-manager" (include "praesto.fullname" .)) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "praesto.managerRoleName" -}}
{{- printf "%s-manager-role" (include "praesto.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "praesto.metricsAuthRoleName" -}}
{{- printf "%s-metrics-auth-role" (include "praesto.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "praesto.metricsReaderRoleName" -}}
{{- printf "%s-metrics-reader" (include "praesto.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "praesto.webhookServiceName" -}}
{{- printf "%s-webhook-service" (include "praesto.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "praesto.metricsServiceName" -}}
{{- printf "%s-controller-manager-metrics-service" (include "praesto.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "praesto.issuerName" -}}
{{- default (printf "%s-selfsigned-issuer" (include "praesto.fullname" .)) .Values.webhooks.certManager.issuer.name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "praesto.certificateName" -}}
{{- default (printf "%s-serving-cert" (include "praesto.fullname" .)) .Values.webhooks.certManager.certificate.name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "praesto.webhookCABundleAnnotation" -}}
{{ include "praesto.namespace" . }}/{{ include "praesto.certificateName" . }}
{{- end -}}

{{- define "praesto.csiDriverName" -}}
{{- default "csi.praesto.io" .Values.csi.driverName -}}
{{- end -}}

{{- define "praesto.csiNodeName" -}}
{{- printf "%s-csi-node" (include "praesto.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "praesto.csiServiceAccountName" -}}
{{- if .Values.csi.serviceAccount.create -}}
{{- default (include "praesto.csiNodeName" .) .Values.csi.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.csi.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "praesto.csiSelectorLabels" -}}
app.kubernetes.io/name: {{ include "praesto.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: csi-node
{{- end -}}
