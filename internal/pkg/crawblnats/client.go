// Package crawblnats provides a thin wrapper around the NATS client for the
// Crawbl orchestrator. It manages JetStream connection lifecycle and
// provides typed Publish/Subscribe operations for the usage event stream.
package crawblnats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Config holds NATS connection parameters.
type Config struct {
	// URL is the NATS server URL (e.g., "nats://localhost:4222").
	URL string
	// StreamName is the JetStream stream name for usage events.
	StreamName string
	// SubjectPrefix is the subject prefix for usage events (e.g., "crawbl.usage.v1").
	SubjectPrefix string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		URL:           "nats://localhost:4222",
		StreamName:    "CRAWBL_USAGE",
		SubjectPrefix: "crawbl.usage.v1",
	}
}

// Client wraps a NATS connection with JetStream for usage event publishing.
type Client struct {
	conn   *nats.Conn
	js     jetstream.JetStream
	config Config
	logger *slog.Logger
}

// Connect establishes a NATS connection and ensures the JetStream stream exists.
// Returns nil Client and nil error if config.URL is empty (NATS disabled).
func Connect(ctx context.Context, cfg Config, logger *slog.Logger) (*Client, error) {
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
		MaxAge:    7 * 24 * time.Hour, // 7 day retention
	})
	if err != nil {
		logger.Warn("NATS stream setup failed (publishing will still work)", "error", err.Error())
	}

	logger.Info("NATS connected", "url", cfg.URL, "stream", cfg.StreamName)

	return &Client{
		conn:   nc,
		js:     js,
		config: cfg,
		logger: logger,
	}, nil
}

// Publish sends a JSON-encoded message to the usage subject for a workspace.
func (c *Client) Publish(ctx context.Context, workspaceID string, payload any) error {
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
// Drain() is async — it signals the connection to flush and close in the
// background. We poll conn.Status() for up to 5 seconds to give in-flight
// messages a chance to flush before the process exits.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	if err := c.conn.Drain(); err != nil {
		// Drain failed (e.g. already closed); fall back to hard close.
		c.conn.Close()
		return nil
	}
	// Wait for drain to complete (status transitions to CLOSED).
	const pollInterval = 100 * time.Millisecond
	const maxWait = 5 * time.Second
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if c.conn.Status() == nats.CLOSED {
			return nil
		}
		time.Sleep(pollInterval)
	}
	// Timed out waiting — force close to avoid leaking the connection.
	c.conn.Close()
	return nil
}

// JetStream returns the underlying JetStream context for consumers.
func (c *Client) JetStream() jetstream.JetStream {
	if c == nil {
		return nil
	}
	return c.js
}
