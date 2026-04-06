package grpc

import "fmt"

// ClusterTarget builds a Kubernetes in-cluster DNS target for gRPC dialing.
// Format: "<service>.<namespace>.svc.cluster.local:<port>"
func ClusterTarget(service, namespace string, port int32) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d", service, namespace, port)
}
