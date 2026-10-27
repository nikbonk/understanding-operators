package controller

import (
	"context"
	"fmt"
	"maps"
	"strings"

	appv1alpha1 "github.com/nikbonk/understanding-operators/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// service labels
const (
	labelInstance = "app.kubernetes.io/instance"
	labelName     = "app.kubernetes.io/name"
)

// labelsFor returns the labels shared by everything managed on behalf of app.
// This map is the contract between the Service's Spec.Selector and (later) the
// Deployment's pod template labels. If the two ever diverge, the Service gets no
// Endpoints and nothing reports an error, so both callers must read it from here.
func labelsFor(app *appv1alpha1.App) map[string]string {
	return map[string]string{
		labelInstance: app.Name,
		labelName:     "nginx",
	}
}

// desiredService builds the Service this controller wants for app. It returns a
// fresh object on every call, so one result can be handed to Create while another
// is used for comparison without the two interfering.
//
// Spec.Type and Spec.ClusterIP are deliberately left unset: the API server
// defaults them, and a desired object that set them itself would either fight the
// server or report drift on every pass.
func desiredService(app *appv1alpha1.App) *corev1.Service {
	port := int32(*app.Spec.Port)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: labelsFor(app),
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       port,
				TargetPort: intstr.FromInt32(port),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

// serviceDrift reports how svc differs from the Service this controller wants.
// An empty result means the object is in sync.
//
// Only the fields this controller owns are compared. A whole-Spec comparison
// would never agree, because the API server defaults clusterIP, clusterIPs, type,
// sessionAffinity, ipFamilies, ipFamilyPolicy and internalTrafficPolicy, none of
// which desiredService sets.
//
// The returned strings are human-readable and are reused verbatim for both the
// log line and the condition message.
//
// Invariant: the fields compared here must be exactly the fields the update path
// assigns. Compare more than you assign and the same drift is re-detected on
// every pass; assign more than you compare and the update overwrites fields that
// were never inspected.
func serviceDrift(app *appv1alpha1.App, svc *corev1.Service) []string {
	var drift []string

	// Compare against the same builder that creates the Service, so this check
	// follows any future change to desiredService without being edited.
	expectedService := desiredService(app)

	if !maps.Equal(svc.Spec.Selector, expectedService.Spec.Selector) {
		drift = append(drift, fmt.Sprintf("spec.selector: expected %v, found %v", expectedService.Spec.Selector, svc.Spec.Selector))
	}

	// Indexing Ports[0] is only safe once the length is known, and the port count
	// is part of the contract, so a wrong number is itself drift.
	switch {
	case len(svc.Spec.Ports) == 0:
		drift = append(drift, "spec.ports: expected 1 port, found none")
	case len(svc.Spec.Ports) != len(expectedService.Spec.Ports):
		drift = append(drift, fmt.Sprintf("spec.ports: expected %v port, found %d", len(expectedService.Spec.Ports), len(svc.Spec.Ports)))
	default:
		want := expectedService.Spec.Ports[0]
		found := svc.Spec.Ports[0]

		if want.Port != found.Port {
			drift = append(drift, fmt.Sprintf("spec.ports[0].port: expected %v, found %v", want.Port, found.Port))
		}

		// IntVal compares the integer representation. A Service edited to
		// targetPort: "80" (a string) therefore counts as drift and gets rewritten in
		// its canonical integer form. IntValue() would instead treat both as port 80.
		if want.TargetPort.IntVal != found.TargetPort.IntVal {
			drift = append(drift, fmt.Sprintf("spec.ports[0].targetPort: expected %v, found %v", want.TargetPort.IntVal, found.TargetPort.IntVal))
		}

		if want.Name != found.Name {
			drift = append(drift, fmt.Sprintf("spec.ports[0].name: expected %v, found %v", want.Name, found.Name))
		}

		if want.Protocol != found.Protocol {
			drift = append(drift, fmt.Sprintf("spec.ports[0].protocol: expected %v, found %v", want.Protocol, found.Protocol))
		}
	}

	return drift
}

// ensureService makes the cluster's Service match the one derived from app. It is
// idempotent: running it repeatedly converges rather than accumulating changes.
//
// Get-or-create-or-update: a missing Service is created, a drifted one is
// corrected field by field, and an in-sync one produces no API writes at all.
func (r *AppReconciler) ensureService(ctx context.Context, app *appv1alpha1.App) error {
	log := logf.FromContext(ctx)
	var svc corev1.Service
	reqSvc := types.NamespacedName{Namespace: app.Namespace, Name: app.Name}

	// A typed NotFound means "not created yet"; any other error is a genuine
	// failure. Collapsing the two either never creates the Service or hides the
	// real cause behind a misleading create error.
	err := r.Get(ctx, reqSvc, &svc)
	if apierrors.IsNotFound(err) {
		log.Info("Service not found, will now be created")
		desired := desiredService(app)

		// The owner reference makes Kubernetes garbage-collect the Service when the
		// App is deleted, and lets Owns(&corev1.Service{}) map Service events back to
		// the owning App.
		if err := controllerutil.SetControllerReference(app, desired, r.Scheme); err != nil {
			log.Error(err, "Failed to set owner on Resource")
			return err
		}

		if err := r.Create(ctx, desired); err != nil {
			log.Error(err, "Failed to create Service")
			return err
		}

		log.Info("Created desired Service", "name", desired.Name)

		// Return instead of falling through to the drift check: the server has since
		// filled in clusterIP, type and ipFamilies, so desired is not what is stored.
		// The create event drives the next pass, which compares against the real object.
		return r.setCondition(ctx, app, metav1.Condition{
			Type:    typeProgressingApp,
			Status:  metav1.ConditionTrue,
			Reason:  "ServiceCreated",
			Message: "Service created"})
	}

	if err != nil {
		log.Error(err, "Failed to get Service resource")
		return err
	}

	drift := serviceDrift(app, &svc)
	if len(drift) != 0 {
		// Assign only the fields serviceDrift compared. svc.Spec = desired.Spec would
		// also blank the immutable clusterIP, which the API server rejects, so the
		// update would fail on every retry and never converge.
		desired := desiredService(app)
		svc.Spec.Selector = desired.Spec.Selector
		svc.Spec.Ports = desired.Spec.Ports

		if err := r.Update(ctx, &svc); err != nil {
			log.Error(err, "Failed to reconcile Service")
			return err
		}

		// One string, two consumers: the log for whoever is debugging, and the
		// condition message that surfaces in kubectl describe app.
		msg := strings.Join(drift, "; ")
		log.Info("Corrected Service drift", "diffs", drift)

		return r.setCondition(ctx, app, metav1.Condition{
			Type:    typeProgressingApp,
			Status:  metav1.ConditionTrue,
			Reason:  "ServiceUpdated",
			Message: msg,
		})
	}

	// In sync. setCondition returns early when the stored condition already holds
	// these values, so a steady pass performs no API writes at all.
	return r.setCondition(ctx, app, metav1.Condition{
		Type:    typeProgressingApp,
		Status:  metav1.ConditionFalse,
		Reason:  "ServiceReconciled",
		Message: "Service is in sync",
	})
}
