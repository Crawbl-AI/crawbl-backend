package realtime

import "context"

func (NopBroadcaster) EmitToWorkspace(_ context.Context, _ string, _ string, _ any) {}
func (NopBroadcaster) EmitMessageNew(_ context.Context, _ string, _ any)             {}
func (NopBroadcaster) EmitMessageUpdated(_ context.Context, _ string, _ any)         {}
func (NopBroadcaster) EmitAgentStatus(_ context.Context, _ string, _ string, _ string, _ ...string) {
}
func (NopBroadcaster) EmitMessageChunk(_ context.Context, _ string, _ MessageChunkPayload) {}
func (NopBroadcaster) EmitMessageDone(_ context.Context, _ string, _ MessageDonePayload)   {}
func (NopBroadcaster) EmitAgentTool(_ context.Context, _ string, _ AgentToolPayload)          {}
func (NopBroadcaster) EmitMessageStatus(_ context.Context, _ string, _ MessageStatusPayload) {}
