package local

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/storage"
)

// FileReadOptions is the argument shape for the file_read tool.
type FileReadOptions struct {
	// Key is the object key the user (or orchestrator) stored the
	// file under. Examples: "uploads/trip-itinerary.md",
	// "notes/2026-04-05.txt". Slashes are allowed.
	Key string `json:"key"`
}

// FileReadResult is the tool output. Content is returned as text
// when the blob's MIME type is textual; otherwise the handler wraps
// it as base64 so the LLM can at least see the file exists and
// decide how to react. Keeping the result struct flat helps the
// LLM cite the source in its reply.
type FileReadResult struct {
	Key         string `json:"key"`
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
	Encoding    string `json:"encoding"` // "text" or "base64"
	SizeBytes   int    `json:"size_bytes"`
}

// FileWriteOptions is the argument shape for the file_write tool.
type FileWriteOptions struct {
	Key         string `json:"key"`
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"`
}

// FileWriteResult echoes the full object key so the LLM has a stable
// pointer it can refer back to in subsequent turns.
type FileWriteResult struct {
	Key         string `json:"key"`
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int    `json:"size_bytes"`
}

// FileRead fetches an object from Spaces, scoped to the runtime
// pod's workspace. The workspace ID is captured at tool construction
// time (see agents.NewFileReadTool) so agents cannot read another
// workspace's files by crafting an alternative workspace_id in the
// tool arguments.
func FileRead(ctx context.Context, client *storage.SpacesClient, workspaceID string, opts FileReadOptions) (FileReadResult, error) {
	if client == nil {
		return FileReadResult{}, errors.New("file_read: storage is not configured on this runtime")
	}
	key := strings.TrimSpace(opts.Key)
	if key == "" {
		return FileReadResult{}, errors.New("file_read: key is required")
	}
	body, contentType, err := client.Get(ctx, workspaceID, key)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			return FileReadResult{Key: key}, fmt.Errorf("file_read: no file at key %q in this workspace", key)
		}
		return FileReadResult{Key: key}, fmt.Errorf("file_read: %w", err)
	}
	result := FileReadResult{
		Key:         key,
		ContentType: contentType,
		SizeBytes:   len(body),
	}
	if isTextualContentType(contentType) {
		result.Content = string(body)
		result.Encoding = "text"
	} else {
		result.Content = base64.StdEncoding.EncodeToString(body)
		result.Encoding = "base64"
	}
	return result, nil
}

// FileWrite persists an object to Spaces under the runtime pod's
// workspace prefix. Like FileRead, the workspace ID is captured at
// construction time — agents cannot target another workspace.
func FileWrite(ctx context.Context, client *storage.SpacesClient, workspaceID string, opts FileWriteOptions) (FileWriteResult, error) {
	if client == nil {
		return FileWriteResult{}, errors.New("file_write: storage is not configured on this runtime")
	}
	key := strings.TrimSpace(opts.Key)
	if key == "" {
		return FileWriteResult{}, errors.New("file_write: key is required")
	}
	if strings.TrimSpace(opts.Content) == "" {
		return FileWriteResult{}, errors.New("file_write: content is required")
	}
	body := []byte(opts.Content)
	contentType := strings.TrimSpace(opts.ContentType)
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	objectKey, err := client.Put(ctx, workspaceID, key, body, contentType)
	if err != nil {
		return FileWriteResult{Key: key}, fmt.Errorf("file_write: %w", err)
	}
	return FileWriteResult{
		Key:         key,
		ObjectKey:   objectKey,
		ContentType: contentType,
		SizeBytes:   len(body),
	}, nil
}

// isTextualContentType returns true for MIME types we are willing to
// hand back to the LLM as a UTF-8 string. Anything else is base64-
// encoded at the boundary so the LLM does not see arbitrary binary
// bytes in its context window.
func isTextualContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "" {
		return true // fall through to text when the server did not set a type
	}
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch {
	case strings.HasPrefix(ct, "application/json"),
		strings.HasPrefix(ct, "application/xml"),
		strings.HasPrefix(ct, "application/yaml"),
		strings.HasPrefix(ct, "application/x-yaml"),
		strings.HasPrefix(ct, "application/javascript"),
		strings.HasPrefix(ct, "application/x-ndjson"):
		return true
	}
	return false
}
