// Package queue owns every River-backed background job, periodic schedule,
// and outbound event publisher used by the orchestrator.
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// DefaultNATSConfig returns a NATSConfig with sensible defaults.
func DefaultNATSConfig() NATSConfig {
	return NATSConfig{
		URL:           "nats://localhost:4222",
		StreamName:    "CRAWBL_USAGE",
		SubjectPrefix: "crawbl.usage.v1",
	}
}

// ConnectNATS establishes a NATS connection and ensures the JetStream stream
// exists. Returns nil client and nil error if cfg.URL is empty (NATS disabled).
func ConnectNATS(ctx context.Context, cfg NATSConfig, logger *slog.Logger) (*NATSClient, error) {
	if cfg.URL == "" {
		if logger != nil {
			logger.Info("NATS disabled (no URL configured)")
		}
		return nil, nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	nc, err := nats.Connect(cfg.URL,
		nats.Name("crawbl-orchestrator"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				logger.Warn("NATS disconnected", "error", err.Error())
			}
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			logger.Info("NATS reconnected")
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats jetstream: %w", err)
	}

	// Ensure the usage stream exists (idempotent).
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      cfg.StreamName,
		Subjects:  []string{cfg.SubjectPrefix + ".>"},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy,
		MaxAge:    7 * 24 * time.Hour,
	})
	if err != nil {
		logger.Warn("NATS stream setup failed (publishing will still work)", "error", err.Error())
	}

	logger.Info("NATS connected", "url", cfg.URL, "stream", cfg.StreamName)

	return &NATSClient{
		conn:   nc,
		js:     js,
		config: cfg,
		logger: logger,
	}, nil
}

// Publish sends a JSON-encoded message to the usage subject for a workspace.
func (c *NATSClient) Publish(ctx context.Context, workspaceID string, payload any) error {
	if c == nil || c.js == nil {
		return nil // NATS disabled
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("nats publish marshal: %w", err)
	}

	subject := fmt.Sprintf("%s.%s", c.config.SubjectPrefix, workspaceID)
	_, err = c.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("nats publish to %s: %w", subject, err)
	}

	return nil
}

// Close drains buffered publishes and then closes the NATS connection.
func (c *NATSClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	if err := c.conn.Drain(); err != nil {
		c.conn.Close()
		return nil
	}
	const pollInterval = 100 * time.Millisecond
	const maxWait = 5 * time.Second
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if c.conn.Status() == nats.CLOSED {
			return nil
		}
		time.Sleep(pollInterval)
	}
	c.conn.Close()
	return nil
}

// JetStream returns the underlying JetStream context for consumers.
func (c *NATSClient) JetStream() jetstream.JetStream {
	if c == nil {
		return nil
	}
	return c.js
}
