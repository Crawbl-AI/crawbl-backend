package realtime

import "context"

// NopBroadcaster implements Broadcaster as a null-object: every method is an
// intentional no-op. Use it when realtime is disabled (no Redis) so that all
// downstream services remain functional without needing nil checks.

func (NopBroadcaster) EmitToWorkspace(_ context.Context, _ string, _ string, _ any) {
	// No-op: NopBroadcaster drops all realtime events.
}
func (NopBroadcaster) EmitMessageNew(_ context.Context, _ string, _ any) {
	// No-op: NopBroadcaster drops all realtime events.
}
func (NopBroadcaster) EmitMessageUpdated(_ context.Context, _ string, _ any) {
	// No-op: NopBroadcaster drops all realtime events.
}
func (NopBroadcaster) EmitAgentStatus(_ context.Context, _ string, _ string, _ string, _ ...string) {
	// No-op: NopBroadcaster drops all realtime events.
}
func (NopBroadcaster) EmitMessageChunk(_ context.Context, _ string, _ MessageChunkPayload) {
	// No-op: NopBroadcaster drops all realtime events.
}
func (NopBroadcaster) EmitMessageDone(_ context.Context, _ string, _ MessageDonePayload) {
	// No-op: NopBroadcaster drops all realtime events.
}
func (NopBroadcaster) EmitAgentTool(_ context.Context, _ string, _ AgentToolPayload) {
	// No-op: NopBroadcaster drops all realtime events.
}
func (NopBroadcaster) EmitMessageStatus(_ context.Context, _ string, _ MessageStatusPayload) {
	// No-op: NopBroadcaster drops all realtime events.
}
func (NopBroadcaster) EmitAgentDelegation(_ context.Context, _ string, _ AgentDelegationPayload) {
	// No-op: NopBroadcaster drops all realtime events.
}
func (NopBroadcaster) EmitArtifactUpdated(_ context.Context, _ string, _ ArtifactEventPayload) {
	// No-op: NopBroadcaster drops all realtime events.
}
func (NopBroadcaster) EmitWorkflowEvent(_ context.Context, _ string, _ string, _ WorkflowEventPayload) {
	// No-op: NopBroadcaster drops all realtime events.
}
func (NopBroadcaster) EmitUsageUpdate(_ context.Context, _ string, _ UsageUpdatePayload) {
	// No-op: NopBroadcaster drops all realtime events.
}
