package indexes

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

const (
	UserIDKey           = ".spec.userId"
	RuntimeNamespaceKey = ".spec.placement.runtimeNamespace"
)

func Setup(mgr ctrl.Manager) error {
	indexer := mgr.GetFieldIndexer()

	if err := indexer.IndexField(context.Background(), &crawblv1alpha1.UserSwarm{}, UserIDKey, func(rawObj client.Object) []string {
		sw, ok := rawObj.(*crawblv1alpha1.UserSwarm)
		if !ok || sw.Spec.UserID == "" {
			return nil
		}
		return []string{sw.Spec.UserID}
	}); err != nil {
		return err
	}

	if err := indexer.IndexField(context.Background(), &crawblv1alpha1.UserSwarm{}, RuntimeNamespaceKey, func(rawObj client.Object) []string {
		sw, ok := rawObj.(*crawblv1alpha1.UserSwarm)
		if !ok {
			return nil
		}
		return []string{runtimeNamespace(sw)}
	}); err != nil {
		return err
	}

	return nil
}

func runtimeNamespace(sw *crawblv1alpha1.UserSwarm) string {
	if sw.Spec.Placement.RuntimeNamespace != "" {
		return sw.Spec.Placement.RuntimeNamespace
	}
	return crawblv1alpha1.DefaultRuntimeNamespace
}
