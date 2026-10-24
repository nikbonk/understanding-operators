/*
Copyright 2026.

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

	appv1alpha1 "github.com/nikbonk/understanding-operators/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// AppReconciler reconciles a App object
type AppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// conditions
const (
	typeReadyApp       = "Ready"
	typeProgressingApp = "Progressing"
	typeDegradedApp    = "Degraded"
)

// +kubebuilder:rbac:groups=app.nikbonk.cc,resources=apps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=app.nikbonk.cc,resources=apps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=app.nikbonk.cc,resources=apps/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the App object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.25.0/pkg/reconcile
func (r *AppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	var app appv1alpha1.App

	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		if apierrors.IsNotFound(err) {
			// NotFound can mean it was either already deleted or never created
			log.Info("App resource not found.")
			return ctrl.Result{}, nil
		}
		// Couldn't read the object for some reason
		log.Error(err, "Failed to get resource.")
		return ctrl.Result{}, err
	}

	// If new resource it will have no conditions so we initialize them first
	if len(app.Status.Conditions) == 0 {
		changed := meta.SetStatusCondition(&app.Status.Conditions, metav1.Condition{
			Type:               typeProgressingApp,
			Status:             metav1.ConditionUnknown,
			ObservedGeneration: app.Generation,
			Reason:             "Reconciling",
			Message:            "Started reconciliation",
		})

		if err := r.Status().Update(ctx, &app); err != nil {
			log.Error(err, "Failed to update App status", "type", typeProgressingApp, "reason", "Reconciling")
			return ctrl.Result{}, err
		}

		log.V(1).Info("Initialized status condition", "type", typeProgressingApp,
			"reason", "Reconciling", "changed", changed)
	}

	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		log.Error(err, "Failed to re-fetch App.")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appv1alpha1.App{}).
		Named("app").
		Complete(r)
}
