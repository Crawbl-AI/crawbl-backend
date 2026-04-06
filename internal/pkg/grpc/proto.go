package grpc

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// FormatProtoTimestamp renders a google.protobuf.Timestamp to an RFC3339
// UTC string. Returns "" for nil or zero timestamps so the caller's JSON
// layer can omit the field.
func FormatProtoTimestamp(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	t := ts.AsTime()
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
