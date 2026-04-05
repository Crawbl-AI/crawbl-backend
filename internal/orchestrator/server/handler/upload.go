package handler

import "net/http"

// FileUpload handles file uploads for chat attachments and avatars.
// POST /v1/uploads
// This is a mock implementation — real file storage (S3/DO Spaces) comes later.
func FileUpload(c *Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Mock: return a fake upload response
		WriteSuccess(w, http.StatusOK, map[string]any{
			"id":       "mock-upload-id",
			"url":      "https://cdn.crawbl.com/uploads/mock-file.png",
			"filename": "mock-file.png",
			"size":     0,
		})
	}
}
