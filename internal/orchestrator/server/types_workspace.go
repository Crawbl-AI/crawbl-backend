package server

import "time"

type workspaceResponse struct {
	ID        string                    `json:"id"`
	Name      string                    `json:"name"`
	CreatedAt time.Time                 `json:"createdAt"`
	UpdatedAt time.Time                 `json:"updatedAt"`
	Runtime   *workspaceRuntimeResponse `json:"runtime,omitempty"`
}

type workspaceRuntimeResponse struct {
	Status   string `json:"status"`
	Phase    string `json:"phase"`
	Verified bool   `json:"verified"`
}
