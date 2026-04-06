package grpc

import "time"

const (
	// DefaultDialTimeout caps the time a single gRPC dial attempt will
	// block before returning an error. Short so an unreachable pod
	// surfaces fast.
	DefaultDialTimeout = 5 * time.Second

	// DefaultCallTimeout bounds the duration of a non-streaming gRPC
	// call (e.g. Memory RPCs). Streaming calls use the request's own
	// context deadline.
	DefaultCallTimeout = 90 * time.Second
)
