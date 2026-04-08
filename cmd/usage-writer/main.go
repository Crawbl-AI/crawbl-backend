// Package main implements the usage-writer service that consumes token
// usage events from NATS JetStream and batch-inserts them into ClickHouse.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// usageEvent matches the JSON published by the orchestrator's usagepublisher.
type usageEvent struct {
	EventID             string  `json:"event_id"`
	EventTime           string  `json:"event_time"`
	UserID              string  `json:"user_id"`
	WorkspaceID         string  `json:"workspace_id"`
	ConversationID      string  `json:"conversation_id"`
	MessageID           string  `json:"message_id"`
	AgentID             string  `json:"agent_id"`
	AgentDBID           string  `json:"agent_db_id"`
	Model               string  `json:"model"`
	Provider            string  `json:"provider"`
	PromptTokens        int32   `json:"prompt_tokens"`
	CompletionTokens    int32   `json:"completion_tokens"`
	TotalTokens         int32   `json:"total_tokens"`
	ToolUsePromptTokens int32   `json:"tool_use_prompt_tokens"`
	ThoughtsTokens      int32   `json:"thoughts_tokens"`
	CachedTokens        int32   `json:"cached_tokens"`
	CostUSD             float64 `json:"cost_usd"`
	CallSequence        int32   `json:"call_sequence"`
	TurnID              string  `json:"turn_id"`
	SessionID           string  `json:"session_id"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	if err := run(ctx); err != nil {
		slog.Error("usage-writer failed", "error", err)
		cancel()
		os.Exit(1)
	}
	cancel()
}

func run(ctx context.Context) error {
	logger := slog.Default()

	// ClickHouse connection
	chDSN := envOrDefault("CLICKHOUSE_DSN", "clickhouse://localhost:9000/default")
	chDB, err := sql.Open("clickhouse", chDSN)
	if err != nil {
		return fmt.Errorf("clickhouse connect failed: %w", err)
	}
	defer func() { _ = chDB.Close() }()
	if err := chDB.PingContext(ctx); err != nil {
		return fmt.Errorf("clickhouse ping failed: %w", err)
	}
	slog.Info("clickhouse connected")

	// NATS connection
	natsURL := envOrDefault("NATS_URL", "nats://localhost:4222")
	nc, err := nats.Connect(natsURL,
		nats.Name("usage-writer"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return fmt.Errorf("nats connect failed: %w", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("jetstream init failed: %w", err)
	}

	streamName := envOrDefault("NATS_STREAM", "CRAWBL_USAGE")
	consumerName := envOrDefault("NATS_CONSUMER", "usage-writer")

	// Create or get durable consumer
	const maxAckPending = 1024

	consumer, err := js.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Name:          consumerName,
		Durable:       consumerName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxAckPending: maxAckPending,
		FilterSubject: "crawbl.usage.v1.>",
	})
	if err != nil {
		return fmt.Errorf("consumer create failed: %w", err)
	}

	slog.Info("usage-writer started", "stream", streamName, "consumer", consumerName)

	// Batch processing loop
	const (
		batchSize    = 100
		flushTimeout = 500 * time.Millisecond
	)

	batch := make([]usageEvent, 0, batchSize)
	flushTimer := time.NewTimer(flushTimeout)
	defer flushTimer.Stop()

	msgCh := make(chan jetstream.Msg, batchSize)
	pendingMsgs := make([]jetstream.Msg, 0, batchSize)

	// Start consuming
	sub, err := consumer.Messages()
	if err != nil {
		return fmt.Errorf("consume failed: %w", err)
	}
	defer sub.Stop()

	go func() {
		for {
			msg, err := sub.Next()
			if err != nil {
				return
			}
			select {
			case msgCh <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			if len(batch) > 0 {
				flushBatch(ctx, chDB, batch, pendingMsgs, logger)
			}
			slog.Info("usage-writer shutting down")
			return nil

		case msg := <-msgCh:
			var evt usageEvent
			if err := json.Unmarshal(msg.Data(), &evt); err != nil {
				slog.Warn("invalid usage event", "error", err)
				_ = msg.Ack()
				continue
			}
			batch = append(batch, evt)
			pendingMsgs = append(pendingMsgs, msg)

			if len(batch) >= batchSize {
				flushBatch(ctx, chDB, batch, pendingMsgs, logger)
				batch = batch[:0]
				pendingMsgs = pendingMsgs[:0]
				flushTimer.Reset(flushTimeout)
			}

		case <-flushTimer.C:
			if len(batch) > 0 {
				flushBatch(ctx, chDB, batch, pendingMsgs, logger)
				batch = batch[:0]
				pendingMsgs = pendingMsgs[:0]
			}
			flushTimer.Reset(flushTimeout)
		}
	}
}

func flushBatch(ctx context.Context, db *sql.DB, events []usageEvent, msgs []jetstream.Msg, logger *slog.Logger) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		logger.Error("clickhouse tx begin failed", "error", err)
		return
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO llm_usage (
			event_id, event_time, user_id, workspace_id, conversation_id,
			message_id, agent_id, agent_db_id, model, provider,
			prompt_tokens, completion_tokens, total_tokens,
			tool_use_prompt_tokens, thoughts_tokens, cached_tokens,
			cost_usd, call_sequence, turn_id, session_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = tx.Rollback()
		logger.Error("clickhouse prepare failed", "error", err)
		return
	}
	defer func() { _ = stmt.Close() }()

	for i := range events {
		e := &events[i]
		eventTime, _ := time.Parse(time.RFC3339Nano, e.EventTime)
		if eventTime.IsZero() {
			eventTime = time.Now().UTC()
		}
		_, err := stmt.ExecContext(ctx,
			e.EventID, eventTime, e.UserID, e.WorkspaceID, e.ConversationID,
			e.MessageID, e.AgentID, e.AgentDBID, e.Model, e.Provider,
			e.PromptTokens, e.CompletionTokens, e.TotalTokens,
			e.ToolUsePromptTokens, e.ThoughtsTokens, e.CachedTokens,
			e.CostUSD, e.CallSequence, e.TurnID, e.SessionID,
		)
		if err != nil {
			logger.Error("clickhouse insert failed, aborting batch for redelivery", "event_id", e.EventID, "error", err)
			_ = tx.Rollback()
			return
		}
	}

	if err := tx.Commit(); err != nil {
		logger.Error("clickhouse commit failed", "error", err, "batch_size", len(events))
		return
	}

	// Ack all messages after successful commit
	for _, msg := range msgs {
		_ = msg.Ack()
	}

	logger.Info("batch flushed", "events", len(events))
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
