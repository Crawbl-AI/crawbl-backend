package queue

import (
	"time"

	"github.com/google/uuid"
)

// stampEventMetadata fills in a default EventID and EventTime when the
// caller left either blank, and returns the resulting pair. Used by
// every publisher in this package so the uuid + RFC3339Nano boilerplate
// lives in exactly one place.
func stampEventMetadata(id, eventTime string) (string, string) {
	if id == "" {
		id = uuid.NewString()
	}
	if eventTime == "" {
		eventTime = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return id, eventTime
}
