/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=update;

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Pod instance
	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			// Pod not found, may have been deleted
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch Pod")
		return ctrl.Result{}, err
	}

	// Check if the pod already has all the required labels
	requiredLabels := map[string]string{
		"environment":    "production",
		"owningResource": "None",
		"ipAddress":      "pending",
		"nodeName":       "pending",
	}

	// Get owning resource if exists
	if len(pod.OwnerReferences) > 0 {
		requiredLabels["owningResource"] = pod.OwnerReferences[0].Kind
	}

	// Update IP address if available
	if pod.Status.PodIP != "" {
		requiredLabels["ipAddress"] = pod.Status.PodIP
	}

	// Update node name if available
	if pod.Spec.NodeName != "" {
		requiredLabels["nodeName"] = pod.Spec.NodeName
	}

	// Check if all required labels are present and correct
	needsUpdate := false
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
		needsUpdate = true
	}

	for key, requiredValue := range requiredLabels {
		if currentValue, exists := pod.Labels[key]; !exists || currentValue != requiredValue {
			pod.Labels[key] = requiredValue
			needsUpdate = true
		}
	}

	// Only update if changes are needed
	if needsUpdate {
		if err := r.Update(ctx, &pod); err != nil {
			if apierrors.IsConflict(err) {
				// The Pod has been updated since we read it, requeue
				return ctrl.Result{Requeue: true}, nil
			}
			if apierrors.IsNotFound(err) {
				// The Pod has been deleted since we read it, requeue
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(err, "unable to update Pod")
			return ctrl.Result{}, err
		}
		logger.Info("Successfully updated Pod labels", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
	} else {
		logger.Info("Pod labels are up to date", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}
