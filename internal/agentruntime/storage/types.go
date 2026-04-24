package storage

import (
	"errors"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config carries the knobs NewSpacesClient consumes. Populate from
// config.SpacesConfig — this package never reads environment
// variables directly so tests can drive it deterministically.
type Config struct {
	// Endpoint is the Spaces HTTPS URL, e.g.
	// "https://fra1.digitaloceanspaces.com". Required.
	Endpoint string
	// Region is the Spaces region (e.g. "fra1"). Required by the S3
	// client's signing logic even though Spaces ignores it for auth.
	Region string
	// Bucket is the Spaces bucket name that holds every workspace's
	// blobs. Required. Scoped further by (workspace prefix) at the
	// key level so one bucket per cluster is enough.
	Bucket string
	// AccessKey is the Spaces access key ID. Required.
	AccessKey string
	// SecretKey is the Spaces secret access key. Required. Never
	// logged.
	SecretKey string
}

// SpacesClient is the runtime-facing wrapper over the S3 client. Its
// workspace scoping and input validation live here, not in the tool
// handlers, so every call site gets the same safety rails for free.
type SpacesClient struct {
	cfg    Config
	client *s3.Client
}

// ErrNotConfigured is returned when an operation is attempted against
// a nil or unconfigured SpacesClient. Tools wrap it so the LLM sees a
// clear "storage disabled" message instead of a generic nil deref.
var ErrNotConfigured = errors.New("spaces: client is not configured")

// ErrObjectNotFound is returned when GetObject hits a 404 from Spaces.
// Handlers translate this into a user-visible "file not found"
// message so agents can recover by trying a different key.
var ErrObjectNotFound = errors.New("spaces: object not found")

// maxSpacesObjectBytes caps both Get reads and Put writes so a
// pathological upload can never exhaust pod memory. 25 MiB is enough
// for documents, spreadsheets, and images a mobile user might chat
// about; larger blobs should stream through a different endpoint.
const maxSpacesObjectBytes int64 = 25 * 1024 * 1024
