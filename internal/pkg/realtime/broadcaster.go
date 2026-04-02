package realtime

import "context"

func (NopBroadcaster) EmitToWorkspace(_ context.Context, _ string, _ string, _ any) {}
func (NopBroadcaster) EmitMessageNew(_ context.Context, _ string, _ any)             {}
func (NopBroadcaster) EmitMessageUpdated(_ context.Context, _ string, _ any)         {}
func (NopBroadcaster) EmitAgentStatus(_ context.Context, _ string, _ string, _ string, _ ...string) {
}
