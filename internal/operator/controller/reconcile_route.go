package controller

// This file manages the optional HTTPRoute for public exposure of a UserSwarm runtime.
// Most swarms are internal-only (reached via ClusterIP service from the orchestrator),
// but some can be exposed publicly through the shared Gateway API gateway.
// The route is only created when spec.exposure.httpRoute.enabled is true.

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
)

// reconcileHTTPRoute creates, updates, or verifies absence of the HTTPRoute for this swarm.
// If routing is disabled or no host is set, we just confirm the route doesn't exist
// (it may have been created before the user disabled it). If routing is enabled,
// we attach the route to the shared gateway with the configured host and path.
func (r *UserSwarmReconciler) reconcileHTTPRoute(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) error {
	// If routing is disabled, just make sure no stale route exists.
	if !swarm.Spec.Exposure.HTTPRoute.Enabled || swarm.Spec.Exposure.HTTPRoute.Host == "" {
		obj := &gatewayv1.HTTPRoute{}
		err := r.Get(ctx, types.NamespacedName{Namespace: desiredRuntimeNamespace(swarm), Name: httpRouteName(swarm)}, obj)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	// Determine the path match type — defaults to prefix matching.
	pathMatchType := gatewayv1.PathMatchPathPrefix
	if swarm.Spec.Exposure.HTTPRoute.PathMatch == "Exact" {
		pathMatchType = gatewayv1.PathMatchExact
	}
	if swarm.Spec.Exposure.HTTPRoute.PathMatch == "RegularExpression" {
		pathMatchType = gatewayv1.PathMatchRegularExpression
	}

	host := gatewayv1.Hostname(swarm.Spec.Exposure.HTTPRoute.Host)
	path := routePath(swarm)
	port := gatewayv1.PortNumber(runtimePort(swarm))
	gatewayNamespace := gatewayv1.Namespace(routeGatewayNamespace(swarm))
	sectionName := gatewayv1.SectionName(routeSectionName(swarm))

	obj := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      httpRouteName(swarm),
			Namespace: desiredRuntimeNamespace(swarm),
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Labels = labelsFor(swarm)
		if err := controllerutil.SetControllerReference(swarm, obj, r.Scheme); err != nil {
			return err
		}

		// Public exposure attaches each runtime to the shared Gateway instead of
		// spinning up a per-user LoadBalancer — much cheaper and easier to manage.
		obj.Spec.Hostnames = []gatewayv1.Hostname{host}
		obj.Spec.ParentRefs = []gatewayv1.ParentReference{{
			Name:        gatewayv1.ObjectName(routeGatewayName(swarm)),
			Namespace:   &gatewayNamespace,
			SectionName: &sectionName,
		}}
		obj.Spec.Rules = []gatewayv1.HTTPRouteRule{{
			Matches: []gatewayv1.HTTPRouteMatch{{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  &pathMatchType,
					Value: &path,
				},
			}},
			BackendRefs: []gatewayv1.HTTPBackendRef{{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: gatewayv1.ObjectName(serviceName(swarm)),
						Port: &port,
					},
				},
			}},
		}}
		return nil
	})
	return err
}

// routeConditionStatus checks whether the HTTPRoute has been accepted by the gateway controller.
// This matters because a route can exist but not actually work — the gateway might reject it
// due to hostname conflicts, missing TLS certs, etc. We check both "Accepted" and "ResolvedRefs"
// conditions on the parent status.
func (r *UserSwarmReconciler) routeConditionStatus(ctx context.Context, swarm *crawblv1alpha1.UserSwarm) (metav1.ConditionStatus, string, string, error) {
	// If routing is disabled, consider it "ready" so it doesn't block the overall readiness check.
	if !swarm.Spec.Exposure.HTTPRoute.Enabled {
		return metav1.ConditionTrue, conditionReasonDisabled, "public routing is disabled", nil
	}

	var route gatewayv1.HTTPRoute
	if err := r.Get(ctx, types.NamespacedName{Namespace: desiredRuntimeNamespace(swarm), Name: httpRouteName(swarm)}, &route); err != nil {
		if apierrors.IsNotFound(err) {
			return metav1.ConditionFalse, conditionReasonPending, "httproute is not ready yet", nil
		}
		return metav1.ConditionFalse, conditionReasonReconcileError, "failed to load httproute", err
	}

	// Walk through each parent gateway's status to see if this route was accepted.
	// A route needs both "Accepted" (gateway acknowledges it) and "ResolvedRefs"
	// (backend service reference is valid) to be fully functional.
	for _, parent := range route.Status.Parents {
		accepted := false
		resolved := false
		for _, cond := range parent.Conditions {
			switch cond.Type {
			case string(gatewayv1.RouteConditionAccepted):
				accepted = cond.Status == metav1.ConditionTrue
			case string(gatewayv1.RouteConditionResolvedRefs):
				resolved = cond.Status == metav1.ConditionTrue
			}
		}
		if accepted && resolved {
			return metav1.ConditionTrue, conditionReasonReady, "public route is attached to the shared gateway", nil
		}
	}

	return metav1.ConditionFalse, conditionReasonPending, "public route exists but is not yet accepted by the gateway", nil
}
