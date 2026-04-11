// Package e2e — streaming reply step definitions.
//
// These steps assert that the assistant's replies arrive progressively
// over the real-time update channel (text chunks, final message, tool
// activity) within a bounded time window.
//
// TODO: implement step bodies once a Socket.IO Go client library is added
// to go.mod. The server uses github.com/zishang520/socket.io/v2 (namespace
// "/v1", room "workspace:<id>", events: message.chunk / message.done /
// agent.tool). A compatible client library — e.g.
// github.com/zishang520/socket.io-client-go — must be vendored before
// these steps can dial the live server.
//
// All scenarios in test-features/chat/streaming.feature are tagged @wip
// and every step below returns nil (graceful no-op) until that library is
// available. Register this lane by calling registerStreamSteps from
// initScenario in e2e.go once the implementation is complete.
package e2e

import (
	"fmt"
	"log"

	"github.com/cucumber/godog"
)

// registerStreamSteps binds all Gherkin phrases for streaming assertions.
func registerStreamSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^user "([^"]*)" is connected to the live update channel$`, tc.streamConnect)
	sc.Step(`^the assistant should stream at least (\d+) text chunks? to the user within (\d+) seconds?$`, tc.streamAssertChunksReceived)
	sc.Step(`^a final complete message should arrive for the reply within (\d+) seconds?$`, tc.streamAssertFinalReceived)
	sc.Step(`^at least one tool activity event should be received for the reply within (\d+) seconds?$`, tc.streamAssertToolActivity)
}

// streamConnect opens a real-time update channel connection for the named user
// and subscribes to their workspace room.
//
// TODO: dial the Socket.IO server at cfg.BaseURL on namespace "/v1",
// send a workspace.subscribe event with the user's workspace ID, and store
// the client + an event channel on tc. Requires a Socket.IO Go client library.
func (tc *testContext) streamConnect(alias string) error {
	log.Printf("streamConnect: @wip — no Socket.IO client library available (user %q)", alias)
	state := tc.state[alias]
	if state == nil {
		return fmt.Errorf("user %q has no journey state; ensure workspace setup steps ran first", alias)
	}
	// No-op until the client library is vendored.
	return nil
}

// streamAssertChunksReceived asserts that at least minChunks text chunks
// were received over the live update channel within timeoutSecs seconds.
//
// TODO: poll tc.streamEvents until the required number of chunk events
// (message.chunk) arrive or the deadline expires. Requires streamConnect
// to have dialled successfully first.
func (tc *testContext) streamAssertChunksReceived(minChunks, timeoutSecs int) error {
	log.Printf("streamAssertChunksReceived: @wip — no Socket.IO client library available (min=%d, timeout=%ds)", minChunks, timeoutSecs)
	return nil
}

// streamAssertFinalReceived asserts that a final complete-message event
// arrived within timeoutSecs seconds.
//
// TODO: poll tc.streamEvents until a message.done event arrives or the
// deadline expires.
func (tc *testContext) streamAssertFinalReceived(timeoutSecs int) error {
	log.Printf("streamAssertFinalReceived: @wip — no Socket.IO client library available (timeout=%ds)", timeoutSecs)
	return nil
}

// streamAssertToolActivity asserts that at least one tool activity event
// was received within timeoutSecs seconds.
//
// TODO: poll tc.streamEvents until an agent.tool event arrives or the
// deadline expires.
func (tc *testContext) streamAssertToolActivity(timeoutSecs int) error {
	log.Printf("streamAssertToolActivity: @wip — no Socket.IO client library available (timeout=%ds)", timeoutSecs)
	return nil
}
