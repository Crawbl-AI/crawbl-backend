package handler

import (
	"net/http"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httputil"
)

// FileUpload handles file uploads for chat attachments and avatars.
// POST /v1/uploads
// Not yet implemented — real file storage (S3/DO Spaces) comes later.
func FileUpload(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httputil.WriteErrorMessage(w, http.StatusNotImplemented, "file uploads are not yet available")
	}
}
