package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// protoMarshaler is the shared protojson marshal options for all proto
// responses. UseProtoNames emits snake_case field names matching the
// proto definitions and current JSON wire format.
var protoMarshaler = protojson.MarshalOptions{
	UseProtoNames:   true,
	EmitUnpopulated: true,
}

// protoUnmarshaler is the shared protojson unmarshal options for all
// proto request bodies. DiscardUnknown allows forward-compatible clients.
var protoUnmarshaler = protojson.UnmarshalOptions{
	DiscardUnknown: true,
}

// WriteProtoSuccess writes a protobuf message wrapped in the standard
// {"data": ...} envelope. It uses protojson for proto-canonical field
// naming (snake_case via UseProtoNames) then wraps the raw JSON in the
// envelope.
func WriteProtoSuccess(w http.ResponseWriter, status int, msg proto.Message) {
	data, err := protoMarshaler.Marshal(msg)
	if err != nil {
		slog.Error("failed to marshal proto response", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeRawEnvelope(w, status, data)
}

// WriteProtoArraySuccess writes a slice of protobuf messages wrapped in
// {"data": [...]}. Used by list endpoints that return a flat array.
func WriteProtoArraySuccess(w http.ResponseWriter, status int, msgs []proto.Message) {
	items := make([]json.RawMessage, 0, len(msgs))
	for _, msg := range msgs {
		data, err := protoMarshaler.Marshal(msg)
		if err != nil {
			slog.Error("failed to marshal proto array item", slog.String("error", err.Error()))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		items = append(items, data)
	}
	w.Header().Set("Content-Type", httpserver.ContentTypeJSON)
	w.WriteHeader(status)
	envelope := map[string]any{"data": items}
	if err := json.NewEncoder(w).Encode(envelope); err != nil {
		slog.Error("failed to write proto array envelope", slog.String("error", err.Error()))
	}
}

// maxProtoBodySize is the upper bound for request bodies decoded by
// DecodeProtoJSON. Requests larger than 1 MiB are silently truncated,
// which causes an unmarshal error — preventing OOM from oversized payloads.
const maxProtoBodySize = 1 << 20 // 1 MiB

// DecodeProtoJSON reads JSON from the request body and unmarshals it
// into a proto message using protojson (DiscardUnknown enabled).
func DecodeProtoJSON(r *http.Request, msg proto.Message) error {
	defer func() { _ = r.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxProtoBodySize))
	if err != nil {
		return err
	}
	return protoUnmarshaler.Unmarshal(body, msg)
}

// writeRawEnvelope writes pre-marshaled JSON data inside a {"data": ...}
// envelope to the response writer.
func writeRawEnvelope(w http.ResponseWriter, status int, rawJSON []byte) {
	w.Header().Set("Content-Type", httpserver.ContentTypeJSON)
	w.WriteHeader(status)
	// Write {"data":<raw>}\n manually to avoid double-encoding.
	_, _ = w.Write([]byte(`{"data":`))
	_, _ = w.Write(rawJSON)
	_, _ = w.Write([]byte("}\n"))
}
