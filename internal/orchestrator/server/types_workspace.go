package server

import "time"

// workspaceResponse represents a workspace in API responses.
// Workspaces are the primary organizational unit for user data and swarm instances.
type workspaceResponse struct {
	// ID is the unique identifier for the workspace.
	ID string `json:"id"`

	// Name is the display name of the workspace.
	Name string `json:"name"`

	// CreatedAt is the timestamp when the workspace was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is the timestamp when the workspace was last modified.
	UpdatedAt time.Time `json:"updated_at"`

	// Runtime contains the swarm runtime status if the workspace has been provisioned.
	// May be nil if the runtime has not yet been created.
	Runtime *workspaceRuntimeResponse `json:"runtime,omitempty"`
}

// workspaceRuntimeResponse represents the swarm runtime status for a workspace.
// This information allows clients to determine when a workspace is ready for use.
type workspaceRuntimeResponse struct {
	// Status is the current operational status of the runtime (e.g., "running", "pending", "failed").
	Status string `json:"status"`

	// Phase is the Kubernetes deployment phase of the runtime pod.
	Phase string `json:"phase"`

	// Verified indicates whether the swarm has passed health checks and is ready to receive requests.
	// The backend should wait for Verified=true before routing user traffic to the swarm.
	Verified bool `json:"verified"`

	// TotalAgents is the number of agents provisioned in the workspace.
	TotalAgents int `json:"total_agents"`

	// LastMessagePreview is a preview of the most recent message across all workspace conversations.
	LastMessagePreview *lastMessagePreviewResponse `json:"last_message_preview,omitempty"`
}

// lastMessagePreviewResponse represents a preview of the most recent message in a workspace.
type lastMessagePreviewResponse struct {
	// Text is the message text content.
	Text string `json:"text"`

	// SenderName is the display name of the message sender.
	SenderName string `json:"sender_name"`

	// Timestamp is when the message was created.
	Timestamp time.Time `json:"timestamp"`
}
