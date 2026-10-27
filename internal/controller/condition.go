package controller

import (
	"context"

	appv1alpha1 "github.com/nikbonk/understanding-operators/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// setCondition writes cond onto app.Status.Conditions and persists it, but only
// when the stored condition actually differs.
//
// ObservedGeneration is stamped here rather than by callers: it records which
// revision of the spec the condition describes. meta.SetStatusCondition compares
// it too, so a spec change alone counts as a condition change.
//
// app is a pointer on purpose: r.Status().Update decodes the server's response
// back into it, including the new resourceVersion. That is why callers do not
// need to re-fetch the App afterwards.
func (r *AppReconciler) setCondition(ctx context.Context, app *appv1alpha1.App, cond metav1.Condition) error {
	log := logf.FromContext(ctx)

	// Stamp the generation before comparing, then let the apimachinery helper
	// append (Type absent) or update in place (Type present). It only mutates the
	// in-memory slice; nothing is sent yet.
	cond.ObservedGeneration = app.Generation
	changed := meta.SetStatusCondition(&app.Status.Conditions, cond)

	// Skip the API call when nothing differs. An unconditional Status().Update
	// bumps resourceVersion, which triggers another reconcile, which writes again:
	// a hot loop. Returning nil here is success, not a failure.
	if !changed {
		return nil
	}

	// If Updating the Condition resulted in an error return it so the Reconile() can decide how to react
	if err := r.Status().Update(ctx, app); err != nil {
		log.Error(err, "Failed to update App status", "type", cond.Type, "reason", cond.Reason)
		return err
	}

	log.V(1).Info("Updated status condition", "type", cond.Type, "status", cond.Status, "reason", cond.Reason)
	return nil
}
