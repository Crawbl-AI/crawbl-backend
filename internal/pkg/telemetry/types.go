// Package telemetry wires OpenTelemetry metrics export into every Crawbl
// binary that wants to be observable from VictoriaMetrics.
package telemetry

import (
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"
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

// defaultExportIntervalSeconds is the default metrics export interval in seconds.
const defaultExportIntervalSeconds = 30

// Config carries the runtime knobs Init consumes. Populate it from the
// caller's own config package — this package never reads environment
// variables directly so tests can drive it deterministically.
type Config struct {
	// Enabled is the master switch. When false, Init returns a no-op
	// shutdown function and does not touch the global meter provider.
	Enabled bool
	// Endpoint is the OTLP HTTP metrics endpoint URL. Example:
	// "http://victoria-metrics.monitoring.svc.cluster.local:8428/opentelemetry/v1/metrics".
	// Empty disables export even when Enabled=true.
	Endpoint string
	// ServiceName is the service.name resource attribute. Example:
	// "orchestrator" or "crawbl-agent-runtime".
	ServiceName string
	// ServiceVersion is the service.version resource attribute. Pass the
	// binary's linker-injected version string.
	ServiceVersion string
	// Environment is deployment.environment (dev, staging, prod).
	Environment string
	// Namespace is service.namespace. Defaults to "crawbl" when empty.
	Namespace string
	// ExportInterval controls how often the periodic reader pushes
	// accumulated metrics to VictoriaMetrics. Defaults to 30s.
	ExportInterval time.Duration
}

// TurnMetrics is a small bundle of counters and histograms for the
// agent-runtime's per-turn hot path. Build it once at startup and
// call Record on every completed turn. Concurrent-safe.
type TurnMetrics struct {
	turns    metric.Int64Counter
	duration metric.Float64Histogram
	once     sync.Once
	err      error
}
