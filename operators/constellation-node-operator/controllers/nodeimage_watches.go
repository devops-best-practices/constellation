/*
Copyright (c) Edgeless Systems GmbH

SPDX-License-Identifier: AGPL-3.0-only
*/

package controllers

import (
	"context"

	node "github.com/edgelesssys/constellation/operators/constellation-node-operator/internal/node"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	updatev1alpha1 "github.com/edgelesssys/constellation/operators/constellation-node-operator/api/v1alpha1"
	nodemaintenancev1beta1 "github.com/medik8s/node-maintenance-operator/api/v1beta1"
)

// scalingGroupImageChangedPredicate checks if a scaling group has adopted a new node image for future nodes.
func scalingGroupImageChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldScalingGroup, ok := e.ObjectOld.(*updatev1alpha1.ScalingGroup)
			if !ok {
				return false
			}
			newScalingGroup, ok := e.ObjectNew.(*updatev1alpha1.ScalingGroup)
			if !ok {
				return false
			}
			return oldScalingGroup.Status.ImageReference != newScalingGroup.Status.ImageReference
		},
	}
}

// autoscalerEnabledStatusChangedPredicate checks if the autoscaler was either enabled or disabled.
func autoscalerEnabledStatusChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldAutoscalingStrat, ok := e.ObjectOld.(*updatev1alpha1.AutoscalingStrategy)
			if !ok {
				return false
			}
			newAutoscalingStrat, ok := e.ObjectNew.(*updatev1alpha1.AutoscalingStrategy)
			if !ok {
				return false
			}
			return oldAutoscalingStrat.Status.Enabled != newAutoscalingStrat.Status.Enabled
		},
	}
}

// nodeReadyPredicate checks if a node became ready or acquired a providerID.
func nodeReadyPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode, ok := e.ObjectOld.(*corev1.Node)
			if !ok {
				return false
			}
			newNode, ok := e.ObjectNew.(*corev1.Node)
			if !ok {
				return false
			}
			becameReady := !node.Ready(oldNode) && node.Ready(newNode)
			receivedProviderID := len(oldNode.Spec.ProviderID) == 0 && len(newNode.Spec.ProviderID) != 0
			return becameReady || receivedProviderID
		},
	}
}

// nodeMaintenanceSucceededPredicate checks if a node maintenance resource switched its status to "maintenance succeeded".
func nodeMaintenanceSucceededPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode, ok := e.ObjectOld.(*nodemaintenancev1beta1.NodeMaintenance)
			if !ok {
				return false
			}
			newNode, ok := e.ObjectNew.(*nodemaintenancev1beta1.NodeMaintenance)
			if !ok {
				return false
			}
			succeeded := oldNode.Status.Phase != nodemaintenancev1beta1.MaintenanceSucceeded &&
				newNode.Status.Phase == nodemaintenancev1beta1.MaintenanceSucceeded
			return succeeded
		},
	}
}

// findObjectsForScalingGroup requests a reconcile call for the node image referenced by a scaling group.
func (r *NodeImageReconciler) findObjectsForScalingGroup(rawScalingGroup client.Object) []reconcile.Request {
	scalingGroup := rawScalingGroup.(*updatev1alpha1.ScalingGroup)
	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: scalingGroup.Spec.NodeImage}},
	}
}

// findAllNodeImages requests a reconcile call for all node images.
func (r *NodeImageReconciler) findAllNodeImages(_ client.Object) []reconcile.Request {
	var nodeImageList updatev1alpha1.NodeImageList
	err := r.List(context.TODO(), &nodeImageList)
	if err != nil {
		return []reconcile.Request{}
	}
	requests := make([]reconcile.Request, len(nodeImageList.Items))
	for i, item := range nodeImageList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{Name: item.GetName()},
		}
	}
	return requests
}
