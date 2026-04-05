package telemetry

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Env var names Crawbl binaries read to populate a Config. The
// CRAWBL_OTEL_* namespace keeps them separate from the upstream
// OTEL_EXPORTER_OTLP_* conventions so our two signal pipelines
// (metrics to VictoriaMetrics, logs to VictoriaLogs via Fluent Bit)
// can be configured independently.
const (
	EnvEnabled        = "CRAWBL_OTEL_ENABLED"
	EnvEndpoint       = "CRAWBL_OTEL_METRICS_ENDPOINT"
	EnvServiceName    = "CRAWBL_OTEL_SERVICE_NAME"
	EnvServiceVersion = "CRAWBL_OTEL_SERVICE_VERSION"
	EnvEnvironment    = "CRAWBL_OTEL_ENVIRONMENT"
	EnvNamespace      = "CRAWBL_OTEL_NAMESPACE"
	EnvExportInterval = "CRAWBL_OTEL_EXPORT_INTERVAL"
)

// ConfigFromEnv returns a Config populated from the CRAWBL_OTEL_*
// environment variables. defaultServiceName is the binary identifier
// the caller wants used when CRAWBL_OTEL_SERVICE_NAME is empty.
//
// Enabled defaults to true when CRAWBL_OTEL_METRICS_ENDPOINT is set,
// so cluster deployments with the endpoint wired up just work, and
// local dev without the endpoint stays silent.
func ConfigFromEnv(defaultServiceName, defaultVersion string) Config {
	endpoint := strings.TrimSpace(os.Getenv(EnvEndpoint))
	enabled := parseBool(os.Getenv(EnvEnabled), endpoint != "")
	serviceName := strings.TrimSpace(os.Getenv(EnvServiceName))
	if serviceName == "" {
		serviceName = defaultServiceName
	}
	serviceVersion := strings.TrimSpace(os.Getenv(EnvServiceVersion))
	if serviceVersion == "" {
		serviceVersion = defaultVersion
	}
	return Config{
		Enabled:        enabled,
		Endpoint:       endpoint,
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Environment:    strings.TrimSpace(os.Getenv(EnvEnvironment)),
		Namespace:      strings.TrimSpace(os.Getenv(EnvNamespace)),
		ExportInterval: parseDuration(os.Getenv(EnvExportInterval), 0),
	}
}

func parseBool(raw string, fallback bool) bool {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return fallback
	}
	switch s {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func parseDuration(raw string, fallback time.Duration) time.Duration {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second
	}
	return fallback
}
