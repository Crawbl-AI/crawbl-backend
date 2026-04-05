// Package storage wraps the aws-sdk-go-v2 S3 client against DigitalOcean
// Spaces for durable blob storage owned by the crawbl-agent-runtime.
//
// Runtime pods use this to back the file_read / file_write tools: a
// mobile user uploads a file through the orchestrator, the
// orchestrator drops it into Spaces under the workspace prefix, and
// the agent reads it back through the same client. All blobs live
// under a per-workspace key prefix so agents in one workspace cannot
// reach another workspace's files.
//
// The aws-sdk-go-v2 S3 client is used only as a wire-protocol client
// here — there is no AWS dependency. Every S3 API call goes to the
// DigitalOcean Spaces endpoint configured via CRAWBL_SPACES_*, which
// implements the S3 HTTP protocol natively. No AWS IAM, no AWS
// Secrets Manager, no AWS config loaders — Spaces credentials come
// from our own ESO-synced Secret.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config carries the knobs NewSpacesClient consumes. Populate from
// config.SpacesConfig — this package never reads environment
// variables directly so tests can drive it deterministically.
type Config struct {
	// Endpoint is the Spaces HTTPS URL, e.g.
	// "https://fra1.digitaloceanspaces.com". Required.
	Endpoint string
	// Region is the Spaces region (e.g. "fra1"). Required by the S3
	// client's signing logic even though Spaces ignores it for auth.
	Region string
	// Bucket is the Spaces bucket name that holds every workspace's
	// blobs. Required. Scoped further by (workspace prefix) at the
	// key level so one bucket per cluster is enough.
	Bucket string
	// AccessKey is the Spaces access key ID. Required.
	AccessKey string
	// SecretKey is the Spaces secret access key. Required. Never
	// logged.
	SecretKey string
}

// SpacesClient is the runtime-facing wrapper over the S3 client. Its
// workspace scoping and input validation live here, not in the tool
// handlers, so every call site gets the same safety rails for free.
type SpacesClient struct {
	cfg    Config
	client *s3.Client
}

// ErrNotConfigured is returned when an operation is attempted against
// a nil or unconfigured SpacesClient. Tools wrap it so the LLM sees a
// clear "storage disabled" message instead of a generic nil deref.
var ErrNotConfigured = errors.New("spaces: client is not configured")

// ErrObjectNotFound is returned when GetObject hits a 404 from Spaces.
// Handlers translate this into a user-visible "file not found"
// message so agents can recover by trying a different key.
var ErrObjectNotFound = errors.New("spaces: object not found")

// NewSpacesClient constructs an SpacesClient from a Config. Returns
// nil + nil when every field is empty (storage disabled) so main.go
// can treat spaces as optional without a special branch. Returns an
// error when SOME fields are set but others are missing — a partial
// config is always an operator mistake.
func NewSpacesClient(cfg Config) (*SpacesClient, error) {
	if cfg.Endpoint == "" && cfg.Region == "" && cfg.Bucket == "" && cfg.AccessKey == "" && cfg.SecretKey == "" {
		return nil, nil
	}
	var missing []string
	if cfg.Endpoint == "" {
		missing = append(missing, "endpoint")
	}
	if cfg.Region == "" {
		missing = append(missing, "region")
	}
	if cfg.Bucket == "" {
		missing = append(missing, "bucket")
	}
	if cfg.AccessKey == "" {
		missing = append(missing, "access_key")
	}
	if cfg.SecretKey == "" {
		missing = append(missing, "secret_key")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("spaces: missing required config fields: %v", missing)
	}

	client := s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.AccessKey,
			cfg.SecretKey,
			"",
		),
		UsePathStyle: false,
	})
	return &SpacesClient{cfg: cfg, client: client}, nil
}

// Bucket returns the configured bucket name. Exposed for logging and
// for downstream code that wants to render a full `s3://<bucket>/<key>`
// URL to end users.
func (c *SpacesClient) Bucket() string {
	if c == nil {
		return ""
	}
	return c.cfg.Bucket
}

// Get fetches the object at (workspaceID, userKey). workspaceID is
// authoritative and appears as the key prefix so agents cannot read
// another workspace's blobs by crafting a path. userKey is whatever
// the caller wants to use as the file name — slashes are allowed so
// callers can organize blobs hierarchically.
//
// Returns (content, contentType, error). Content is read fully into
// memory; callers that need streaming should add a separate method
// when a real user hits the cap (currently 25 MiB).
func (c *SpacesClient) Get(ctx context.Context, workspaceID, userKey string) ([]byte, string, error) {
	if c == nil || c.client == nil {
		return nil, "", ErrNotConfigured
	}
	if strings.TrimSpace(workspaceID) == "" {
		return nil, "", errors.New("spaces: workspace_id is required")
	}
	if strings.TrimSpace(userKey) == "" {
		return nil, "", errors.New("spaces: key is required")
	}
	key := buildObjectKey(workspaceID, userKey)

	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, "", ErrObjectNotFound
		}
		return nil, "", fmt.Errorf("spaces: get %q: %w", key, err)
	}
	defer out.Body.Close()

	body, err := io.ReadAll(io.LimitReader(out.Body, maxSpacesObjectBytes))
	if err != nil {
		return nil, "", fmt.Errorf("spaces: read body %q: %w", key, err)
	}
	contentType := ""
	if out.ContentType != nil {
		contentType = *out.ContentType
	}
	return body, contentType, nil
}

// Put writes the object at (workspaceID, userKey) with the given
// content and optional MIME type. Overwrites on conflict. An empty
// contentType falls back to the S3 default (application/octet-stream).
//
// Returns the full S3 object key so the caller can log a stable
// reference or hand it back to the LLM.
func (c *SpacesClient) Put(ctx context.Context, workspaceID, userKey string, body []byte, contentType string) (string, error) {
	if c == nil || c.client == nil {
		return "", ErrNotConfigured
	}
	if strings.TrimSpace(workspaceID) == "" {
		return "", errors.New("spaces: workspace_id is required")
	}
	if strings.TrimSpace(userKey) == "" {
		return "", errors.New("spaces: key is required")
	}
	if int64(len(body)) > maxSpacesObjectBytes {
		return "", fmt.Errorf("spaces: object exceeds %d byte cap", maxSpacesObjectBytes)
	}
	key := buildObjectKey(workspaceID, userKey)

	in := &s3.PutObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	if _, err := c.client.PutObject(ctx, in); err != nil {
		return "", fmt.Errorf("spaces: put %q: %w", key, err)
	}
	return key, nil
}

// buildObjectKey composes the workspace-scoped key every Get/Put
// uses: "workspaces/<workspace_id>/files/<user_key>". path.Clean
// strips any leading slashes and "../" traversal attempts from the
// user-supplied portion.
func buildObjectKey(workspaceID, userKey string) string {
	cleaned := path.Clean("/" + userKey)
	if strings.HasPrefix(cleaned, "/") {
		cleaned = strings.TrimPrefix(cleaned, "/")
	}
	return path.Join("workspaces", workspaceID, "files", cleaned)
}

// isNotFound detects the Spaces 404 error shape without importing the
// heavy-weight types package. The S3 SDK wraps NoSuchKey in a smithy
// API error whose Code() is "NoSuchKey"; we match on the substring
// to stay forward-compatible across SDK versions.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "NoSuchKey") || strings.Contains(msg, "NotFound") || strings.Contains(msg, "status code: 404")
}

// maxSpacesObjectBytes caps both Get reads and Put writes so a
// pathological upload can never exhaust pod memory. 25 MiB is enough
// for documents, spreadsheets, and images a mobile user might chat
// about; larger blobs should stream through a different endpoint.
const maxSpacesObjectBytes int64 = 25 * 1024 * 1024
