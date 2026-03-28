package kube

// Ptr returns a pointer to the given value.
// Kubernetes APIs frequently require *int32, *bool, *string, etc.
// This avoids the need for temporary variables just to take an address.
//
//	replicas := kube.Ptr(int32(1))
//	runAsNonRoot := kube.Ptr(true)
func Ptr[T any](v T) *T { return &v }
