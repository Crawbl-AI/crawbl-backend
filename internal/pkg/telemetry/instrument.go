package telemetry

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Meter returns a global meter scoped to the caller's component. The
// scope is attached as the `otel.scope.name` attribute on every
// emitted metric, so VictoriaMetrics can filter by it. Always returns
// a usable Meter even when telemetry is disabled — the global
// provider is a no-op in that case and the instruments silently drop.
func Meter(scope string) metric.Meter {
	return otel.Meter("crawbl/" + scope)
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

// NewTurnMetrics lazily builds the instruments the first time it is
// called. A build failure (rare — only happens when the meter
// provider is misconfigured) is stashed and all subsequent Record
// calls become no-ops so a telemetry fault never crashes the agent.
func NewTurnMetrics() *TurnMetrics {
	return &TurnMetrics{}
}

func (m *TurnMetrics) ensure() {
	m.once.Do(func() {
		meter := Meter("agent-runtime")
		turns, err := meter.Int64Counter(
			"crawbl.agent_runtime.turns",
			metric.WithDescription("Total number of Converse turns processed by the agent runtime."),
			metric.WithUnit("{turn}"),
		)
		if err != nil {
			m.err = err
			return
		}
		duration, err := meter.Float64Histogram(
			"crawbl.agent_runtime.turn_duration",
			metric.WithDescription("Wall-clock duration of a single Converse turn, including LLM and tool calls."),
			metric.WithUnit("s"),
		)
		if err != nil {
			m.err = err
			return
		}
		m.turns = turns
		m.duration = duration
	})
}

// Record emits both the turn counter and the duration histogram for
// a single completed turn. status is a free-form label ("ok",
// "error", "cancelled") so operators can slice by outcome.
func (m *TurnMetrics) Record(ctx context.Context, workspaceID, agentID, status string, start time.Time) {
	if m == nil {
		return
	}
	m.ensure()
	if m.err != nil || m.turns == nil || m.duration == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("workspace_id", workspaceID),
		attribute.String("agent_id", agentID),
		attribute.String("status", status),
	)
	m.turns.Add(ctx, 1, attrs)
	m.duration.Record(ctx, time.Since(start).Seconds(), attrs)
}
