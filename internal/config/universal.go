package config

// UniversalValues configures the built-in universal chart (embedded via
// go:embed and rendered in a later milestone). The schema below covers the
// starter resource set from the spec — Deployment, Service, Ingress,
// ConfigMap, Secret, HPA — and is expanded when the chart lands.
type UniversalValues struct {
	// Image is the container image, e.g. redis:7.
	Image string `json:"image" yaml:"image"`
	// Replicas is the Deployment replica count.
	Replicas int `json:"replicas" yaml:"replicas" default:"1"`
	// Command / Args override the container entrypoint.
	Command []string `json:"command" yaml:"command"`
	Args    []string `json:"args" yaml:"args"`
	// Env are plain environment variables for the container.
	Env map[string]string `json:"env" yaml:"env"`

	// Service, when set, renders a Service.
	Service *UniversalService `json:"service" yaml:"service"`
	// Ingress, when set, renders an Ingress.
	Ingress *UniversalIngress `json:"ingress" yaml:"ingress"`
	// ConfigMap keys become a ConfigMap.
	ConfigMap map[string]string `json:"configMap" yaml:"configMap"`
	// Secret keys become a Secret (values are plain here; secret sources land later).
	Secret map[string]string `json:"secret" yaml:"secret"`
	// HPA, when set, renders a HorizontalPodAutoscaler.
	HPA *UniversalHPA `json:"hpa" yaml:"hpa"`
}

// UniversalService describes the generated Service.
type UniversalService struct {
	Type string `json:"type" yaml:"type" default:"ClusterIP"`
	Port int    `json:"port" yaml:"port"`
	// TargetPort defaults to Port when zero.
	TargetPort int `json:"targetPort" yaml:"targetPort"`
}

// UniversalIngress describes the generated Ingress.
type UniversalIngress struct {
	ClassName string   `json:"className" yaml:"className"`
	Host      string   `json:"host" yaml:"host"`
	Path      string   `json:"path" yaml:"path" default:"/"`
	TLS       bool     `json:"tls" yaml:"tls"`
	Hosts     []string `json:"hosts" yaml:"hosts"`
}

// UniversalHPA describes the generated HorizontalPodAutoscaler.
type UniversalHPA struct {
	MinReplicas int `json:"minReplicas" yaml:"minReplicas" default:"1"`
	MaxReplicas int `json:"maxReplicas" yaml:"maxReplicas"`
	// TargetCPUUtilizationPercentage, when >0, adds a CPU metric.
	TargetCPUUtilizationPercentage int `json:"targetCPUUtilizationPercentage" yaml:"targetCPUUtilizationPercentage"`
}
