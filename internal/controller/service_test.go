package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	appv1alpha1 "github.com/nikbonk/understanding-operators/api/v1alpha1"
)

// These specs need no API server: everything here is pure object manipulation.
// They still boot the suite because they share this package, but they exercise
// serviceDrift directly, which is the point - a drift bug shows up here in
// milliseconds with a readable diff instead of as a confusing cluster assertion.
var _ = Describe("serviceDrift", func() {
	var (
		app *appv1alpha1.App
		svc *corev1.Service
	)

	BeforeEach(func() {
		image, replicas, port := "nginx:1.27", 1, 8080

		app = &appv1alpha1.App{
			ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"},
			Spec: appv1alpha1.AppSpec{
				Image:    &image,
				Replicas: &replicas,
				Port:     &port,
			},
		}

		// Start from the builder's own output, so the baseline stays in sync with
		// desiredService instead of duplicating it in the test.
		svc = desiredService(app)
	})

	It("reports no drift for a freshly built Service", func() {
		Expect(serviceDrift(app, desiredService(app))).To(BeEmpty())
	})

	It("ignores fields the API server defaults", func() {
		// A stored Service always has these set. If serviceDrift compared the whole
		// Spec, every pass would report permanent drift.
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.ClusterIP = "10.0.0.42"
		svc.Spec.ClusterIPs = []string{"10.0.0.42"}
		svc.Spec.SessionAffinity = corev1.ServiceAffinityNone

		Expect(serviceDrift(app, svc)).To(BeEmpty())
	})

	It("reports drift when the App asks for a different port", func() {
		// The stored Service still matches the old spec, so the comparison must be
		// driven by the App, not by whether svc equals some snapshot of itself.
		//
		// Two entries, not one: desiredService derives both Port and TargetPort from
		// Spec.Port, so a port change is two fields of drift.
		changed := 9090
		app.Spec.Port = &changed

		drift := serviceDrift(app, svc)
		Expect(drift).To(HaveLen(2), "drift: %v", drift)
		Expect(drift).To(ContainElement(ContainSubstring("spec.ports[0].port")))
		Expect(drift).To(ContainElement(ContainSubstring("spec.ports[0].targetPort")))
	})

	DescribeTable("reports exactly one field of drift",
		func(mutate func(*corev1.Service), want string) {
			mutate(svc)

			drift := serviceDrift(app, svc)
			Expect(drift).To(HaveLen(1), "drift: %v", drift)
			Expect(drift[0]).To(ContainSubstring(want))
		},
		Entry("selector dropped", func(s *corev1.Service) {
			s.Spec.Selector = nil
		}, "spec.selector"),
		Entry("selector points elsewhere", func(s *corev1.Service) {
			s.Spec.Selector[labelInstance] = "someone-else"
		}, "spec.selector"),
		Entry("all ports removed", func(s *corev1.Service) {
			s.Spec.Ports = nil
		}, "spec.ports"),
		Entry("an extra port appears", func(s *corev1.Service) {
			s.Spec.Ports = append(s.Spec.Ports, corev1.ServicePort{
				Name: "metrics", Port: 9090, Protocol: corev1.ProtocolTCP,
			})
		}, "spec.ports"),
		Entry("port changed", func(s *corev1.Service) {
			s.Spec.Ports[0].Port = 80
		}, "spec.ports[0].port"),
		Entry("targetPort changed", func(s *corev1.Service) {
			s.Spec.Ports[0].TargetPort = intstr.FromInt32(81)
		}, "spec.ports[0].targetPort"),
		Entry("targetPort written as a string", func(s *corev1.Service) {
			// Same numeric value, different representation. The controller rewrites
			// it as an int on purpose; IntValue() would call this in sync.
			s.Spec.Ports[0].TargetPort = intstr.FromString("8080")
		}, "spec.ports[0].targetPort"),
		Entry("port name changed", func(s *corev1.Service) {
			s.Spec.Ports[0].Name = "web"
		}, "spec.ports[0].name"),
		Entry("protocol changed", func(s *corev1.Service) {
			s.Spec.Ports[0].Protocol = corev1.ProtocolUDP
		}, "spec.ports[0].protocol"),
		Entry("label added", func(s *corev1.Service) {
			s.Labels[testLabelKey] = testLabelValue
		}, "metadata.labels"),
	)

	It("reports every drifted field at once", func() {
		svc.Spec.Selector = nil
		svc.Spec.Ports[0].Port = 80
		svc.Spec.Ports[0].Name = "web"
		svc.Labels[testLabelKey] = testLabelValue

		drift := serviceDrift(app, svc)
		Expect(drift).To(HaveLen(4), "drift: %v", drift)
	})
})
