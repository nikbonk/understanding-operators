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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appv1alpha1 "github.com/nikbonk/understanding-operators/api/v1alpha1"
)

const (
	testLabelKey   = "newLabel"
	testLabelValue = "true"
)

var _ = Describe("App Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-resource"
			resourceNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}

		// Declared here, assigned in BeforeEach, so each spec starts from a clean
		// reconciler instead of inheriting the previous spec's state.
		var (
			app      *appv1alpha1.App
			recorder *events.FakeRecorder
			r        *AppReconciler
		)

		// reconcileOnce runs a single pass and returns the error, so a spec can
		// assert on failure modes instead of dying on the first call.
		reconcileOnce := func() error {
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			return err
		}

		// fetchApp re-reads the App from the API server. The controller works on its
		// own copy, so the fixture's app must never be trusted for status assertions.
		fetchApp := func() *appv1alpha1.App {
			fetched := &appv1alpha1.App{}
			ExpectWithOffset(1, k8sClient.Get(ctx, typeNamespacedName, fetched)).To(Succeed())
			return fetched
		}

		// progressing returns the App's Progressing condition, or nil.
		progressing := func() *metav1.Condition {
			return meta.FindStatusCondition(fetchApp().Status.Conditions, typeProgressingApp)
		}

		// drainEvents empties the recorder's channel into a slice. FakeRecorder
		// sends synchronously, so everything emitted so far is already queued.
		drainEvents := func() []string {
			var events []string
			for {
				select {
				case e := <-recorder.Events:
					events = append(events, e)
				default:
					return events
				}
			}
		}

		BeforeEach(func() {
			// The CRD marks image, replicas and port as required, so an App without
			// them never reaches Reconcile: the API server rejects Create first.
			//
			// Local variables rather than ptr.To, so the fixture needs no extra
			// dependency and the address-of syntax stays visible.
			image, replicas, port := "nginx:1.27", 1, 80

			app = &appv1alpha1.App{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: resourceNamespace,
				},
				Spec: appv1alpha1.AppSpec{
					Image:    &image,
					Replicas: &replicas,
					Port:     &port,
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			// Reconcile calls Recorder.Event unconditionally on the first pass, so a
			// nil recorder panics. 64 is far above the handful of events a spec emits;
			// the send is blocking, so too small a buffer deadlocks instead of failing.
			recorder = events.NewFakeRecorder(64)
			r = &AppReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: recorder,
			}
		})

		AfterEach(func() {
			// envtest runs only etcd and kube-apiserver, not kube-controller-manager,
			// so owner-reference garbage collection never fires here. The Service must
			// be removed by hand or it leaks into the next spec.
			By("Cleaning up the Service")
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: resourceNamespace,
			}}
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, svc))).To(Succeed())

			By("Cleaning up the App")
			resource := &appv1alpha1.App{ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: resourceNamespace,
			}}
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, resource))).To(Succeed())
		})

		It("creates the Service on the first pass", func() {
			Expect(reconcileOnce()).To(Succeed())

			var svc corev1.Service
			Expect(k8sClient.Get(ctx, typeNamespacedName, &svc)).To(Succeed())

			// The Service must point at the labels the Deployment's pods will carry.
			Expect(svc.Spec.Selector).To(Equal(labelsFor(app)))
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(80)))
			Expect(svc.Spec.Ports[0].TargetPort.IntVal).To(Equal(int32(80)))
			Expect(svc.Spec.Ports[0].Name).To(Equal("http"))

			// Controlled by, not merely owned: Owns() and garbage collection both
			// hinge on this reference being the controller reference.
			Expect(metav1.IsControlledBy(&svc, app)).To(BeTrue())

			cond := progressing()
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("ServiceCreated"))
			// Set by setCondition, not by the test: it records which spec revision
			// this outcome describes.
			Expect(cond.ObservedGeneration).To(Equal(app.Generation))

			events := drainEvents()

			// Prefixes include the event type because events.FakeRecorder renders
			// "type reason note". A bare "Created" would also match "ServiceCreated",
			// which is how the missing Service-scoped event went unnoticed.
			Expect(events).To(ContainElement(HavePrefix("Normal ReconcileStarted")))
			Expect(events).To(ContainElement(HavePrefix("Normal Created Created Service")))
			Expect(events).To(ContainElement(HavePrefix("Normal ServiceCreated Created Service")))
		})

		It("reports the Service as reconciled on the following pass", func() {
			// The first pass returns after Create, because the server has since
			// defaulted clusterIP and friends. The second pass compares against the
			// stored object.
			Expect(reconcileOnce()).To(Succeed())
			Expect(reconcileOnce()).To(Succeed())

			cond := progressing()
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("ServiceReconciled"))

			drainEvents()
		})

		It("corrects Service drift", func() {
			// Two passes to reach steady state: the first creates the Service and
			// returns early, the second confirms it is in sync. Starting the drift
			// check any earlier would mix "drift" with "not created yet".
			Expect(reconcileOnce()).To(Succeed())
			Expect(reconcileOnce()).To(Succeed())

			// Break two owned fields - one in Spec, one in ObjectMeta - so a single
			// pass has to report and repair both. A user would do this by editing the
			// stored object; writing it back through the API server is what makes the
			// change real, since a local-only edit would leave the cluster untouched
			// and the whole spec would pass without testing anything.
			var svc corev1.Service
			Expect(k8sClient.Get(ctx, typeNamespacedName, &svc)).To(Succeed())
			svc.Spec.Ports[0].Port = 8080
			svc.Labels[testLabelKey] = testLabelValue

			Expect(k8sClient.Update(ctx, &svc)).To(Succeed())

			// One pass is enough here, unlike the create case: the Service exists, so
			// ensureService takes the get-compare-update branch and returns having
			// already corrected the object.
			Expect(reconcileOnce()).To(Succeed())

			// Re-read instead of asserting on svc. svc is the object we wrote, which
			// still says 8080 and still carries newLabel; only the server knows what
			// the controller did to it.
			var corrected corev1.Service
			Expect(k8sClient.Get(ctx, typeNamespacedName, &corrected)).To(Succeed())
			Expect(corrected.Spec.Ports[0].Port).To(Equal(int32(80)))
			Expect(corrected.Labels).To(Equal(labelsFor(app)))

			// The condition reports what this pass did, so it is True: an update
			// happened. Convergence to False/ServiceReconciled is the next pass's job
			// and is covered by the spec above.
			cond := progressing()
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("ServiceUpdated"))
			// serviceDrift builds these strings once and both the log and the
			// condition reuse them, so asserting the message also pins the diff text.
			Expect(cond.Message).To(ContainSubstring("spec.ports[0].port"))
			Expect(cond.Message).To(ContainSubstring("metadata.labels"))

			// Two events, deliberately: one on the Service for kubectl describe svc,
			// one on the App for kubectl describe app. Kubernetes does not propagate
			// child events to the owner, so both are emitted explicitly.
			events := drainEvents()

			Expect(events).To(ContainElement(HavePrefix("Normal Updated Updated Service")))
			Expect(events).To(ContainElement(HavePrefix("Normal ServiceUpdated Service drift corrected")))
		})

		// A steady-state pass must produce no API writes at all. This is the
		// regression test for the dedup gate in setCondition: without the
		// "if !changed { return nil }" early return, every pass bumps the App's
		// resourceVersion, which generates a watch event, which triggers another
		// reconcile, which writes again - a hot loop that nothing else catches
		// because the object always looks correct.
		It("performs no writes when in sync", func() {
			// Reach steady state first, so the pass under test is a genuine no-op
			// rather than the second half of a create.
			Expect(reconcileOnce()).To(Succeed())
			Expect(reconcileOnce()).To(Succeed())

			// resourceVersion is the API server's optimistic-concurrency token. It
			// changes on every write to the object, so comparing it before and after
			// a pass answers "did anything get written?" without guessing which
			// field might have changed.
			appRV := fetchApp().ResourceVersion

			var svc corev1.Service
			Expect(k8sClient.Get(ctx, typeNamespacedName, &svc)).To(Succeed())
			svcRV := svc.ResourceVersion

			// Empty the recorder before the pass under test. The create passes left
			// events queued, and without draining them the emptiness check below
			// would fail for reasons that have nothing to do with this pass.
			drainEvents()

			Expect(reconcileOnce()).To(Succeed())

			Expect(fetchApp().ResourceVersion).To(Equal(appRV))
			Expect(k8sClient.Get(ctx, typeNamespacedName, &svc)).To(Succeed())
			Expect(svc.ResourceVersion).To(Equal(svcRV))

			// No writes means no events either: events are only emitted where
			// something changed or failed, never on the steady-state path.
			Expect(drainEvents()).To(BeEmpty())
		})
	})
})
