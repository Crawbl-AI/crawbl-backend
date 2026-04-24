package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init configures the global OpenTelemetry meter provider to export
// metrics to the configured OTLP HTTP endpoint. It returns a shutdown
// function the caller MUST defer — letting the process exit without
// flushing drops up to one export interval of metrics.
//
// When cfg.Enabled is false or cfg.Endpoint is empty, Init installs
// nothing and returns a no-op shutdown. Callers can stay agnostic
// about whether telemetry is active.
func Init(ctx context.Context, cfg Config, logger *slog.Logger) (shutdown func(context.Context) error, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	noop := func(context.Context) error { return nil }

	if !cfg.Enabled || strings.TrimSpace(cfg.Endpoint) == "" {
		logger.Info("telemetry: metrics export disabled",
			"enabled", cfg.Enabled,
			"endpoint_set", strings.TrimSpace(cfg.Endpoint) != "",
		)
		return noop, nil
	}

	if cfg.ServiceName == "" {
		return noop, fmt.Errorf("telemetry: ServiceName is required when telemetry is enabled")
	}
	if cfg.ExportInterval <= 0 {
		cfg.ExportInterval = defaultExportIntervalSeconds * time.Second
	}
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "crawbl"
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.ServiceNamespace(namespace),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
	)
	if err != nil {
		return noop, fmt.Errorf("telemetry: build resource: %w", err)
	}

	// Parse the endpoint into host + URL path. otlpmetrichttp takes them
	// as separate options because it also supports the path-relative
	// OTEL_EXPORTER_OTLP_METRICS_ENDPOINT convention.
	host, urlPath, insecure := splitOTLPEndpoint(cfg.Endpoint)

	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(host),
		otlpmetrichttp.WithURLPath(urlPath),
		otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression),
	}
	if insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	exporter, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return noop, fmt.Errorf("telemetry: build metric exporter: %w", err)
	}

	reader := metric.NewPeriodicReader(exporter,
		metric.WithInterval(cfg.ExportInterval),
	)
	provider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(reader),
	)
	otel.SetMeterProvider(provider)

	logger.Info("telemetry: metrics export enabled",
		"service_name", cfg.ServiceName,
		"service_version", cfg.ServiceVersion,
		"environment", cfg.Environment,
		"endpoint", cfg.Endpoint,
		"interval", cfg.ExportInterval,
	)

	return provider.Shutdown, nil
}

// splitOTLPEndpoint parses a full URL into the (host, path, insecure)
// triple otlpmetrichttp wants. Scheme is used only to decide whether
// the exporter needs WithInsecure; the otlpmetrichttp client always
// speaks HTTP/1.1 + protobuf regardless.
func splitOTLPEndpoint(raw string) (host, path string, insecure bool) {
	trimmed := strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(trimmed, "http://"):
		insecure = true
		trimmed = strings.TrimPrefix(trimmed, "http://")
	case strings.HasPrefix(trimmed, "https://"):
		insecure = false
		trimmed = strings.TrimPrefix(trimmed, "https://")
	default:
		// Accept a bare host:port with no scheme; assume insecure so
		// local dev can point at an in-cluster port-forward.
		insecure = true
	}
	slash := strings.Index(trimmed, "/")
	if slash < 0 {
		return trimmed, "/opentelemetry/v1/metrics", insecure
	}
	return trimmed[:slash], trimmed[slash:], insecure
}
