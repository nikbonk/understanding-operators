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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// AppReconciler reconciles a App object
type AppReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
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
//  +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

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
	// Shape of every pass: read the App, record that work has started (first pass
	// only), then converge each child resource. Each ensure* helper owns its own
	// create-or-update logic and writes its own conditions.
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

	// A brand-new App has no Progressing condition yet. Record that reconciliation
	// has started. Once the condition exists, later passes must not overwrite the
	// outcome they already recorded.
	if meta.FindStatusCondition(app.Status.Conditions, typeProgressingApp) == nil {
		if err := r.setCondition(ctx, &app, metav1.Condition{
			Type:    typeProgressingApp,
			Status:  metav1.ConditionUnknown,
			Reason:  "Reconciling",
			Message: "Started reconciliation",
		}); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Event(&app, corev1.EventTypeNormal, "ReconcileStarted", "Started reconciling App")
	}

	// Converge the Service: create it if missing, correct it if it drifted.
	// No re-fetch is needed first, because setCondition handed &app to
	// Status().Update, which wrote the new resourceVersion back into this object.
	if err := r.ensureService(ctx, &app); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appv1alpha1.App{}).
		Owns(&corev1.Service{}).
		Named("app").
		Complete(r)
}
