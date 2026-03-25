package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// WriteSuccessResponse writes a JSON success response with the given status code and data.
// The response is wrapped in a success envelope: {"data": <data>}.
// This function sets the Content-Type header to application/json and logs encoding errors.
func WriteSuccessResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(&successResponseEnvelope{Data: data}); err != nil {
		slog.Error("failed to encode success response", slog.String("error", err.Error()))
	}
}

// WriteNoContent writes an HTTP 204 No Content response.
// Use this for successful operations that return no data, such as DELETE requests.
func WriteNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// WriteErrorResponse writes a JSON error response with the given status code and message.
// The response is wrapped in an error envelope: {"error": <message>}.
// This function sets the Content-Type header to application/json and logs encoding errors.
func WriteErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(&errorResponseEnvelope{Error: message}); err != nil {
		slog.Error("failed to encode error response", slog.String("error", err.Error()))
	}
}

// WriteJSONResponse writes a raw JSON response with the given status code and payload.
// Unlike WriteSuccessResponse, this does not wrap the payload in an envelope.
// This function sets the Content-Type header to application/json and logs encoding errors.
func WriteJSONResponse(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to encode JSON response", slog.String("error", err.Error()))
	}
}
