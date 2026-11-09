package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appv1alpha1 "github.com/nikbonk/understanding-operators/api/v1alpha1"
)

var _ = Describe("effectiveNginxConfig", func() {
	DescribeTable("resolves optional fields to concrete values",
		func(cfg *appv1alpha1.NginxConfig, wantServerName, wantMaxBodySize string) {
			got := effectiveNginxConfig(cfg)

			Expect(got.ServerName).To(Equal(wantServerName))
			Expect(got.ClientMaxBodySize).To(Equal(wantMaxBodySize))
		},

		Entry("nil config falls back entirely",
			(*appv1alpha1.NginxConfig)(nil), "_", "1m",
		),
		Entry("empty block falls back entirely",
			&appv1alpha1.NginxConfig{}, "_", "1m",
		),
		Entry("server name set, body size omitted",
			&appv1alpha1.NginxConfig{ServerName: new("example.com")}, "example.com", "1m",
		),
		Entry("server omitted, body size set",
			&appv1alpha1.NginxConfig{ClientMaxBodySize: new("5m")}, "_", "5m",
		),
		Entry("server name explicitly empty",
			&appv1alpha1.NginxConfig{ServerName: new("")}, "_", "1m",
		),
	)
})

var _ = Describe("renderNginxConfig", func() {

	It("renders the server block", func() {
		got := renderNginxConfig(nginxConfValues{
			ServerName:        "example.com",
			ClientMaxBodySize: "5m",
		})

		Expect(got).To(ContainSubstring("listen 80;"))
		Expect(got).To(ContainSubstring("server_name example.com;"))
		Expect(got).To(ContainSubstring("client_max_body_size 5m;"))
		Expect(got).To(ContainSubstring("location / {"))
		Expect(got).To(ContainSubstring("try_files $uri $uri/ =404;"))
		Expect(got).To(HaveSuffix("}\n"))

	})
})

var _ = Describe("desiredNginxConfConfigMap", func() {
	var (
		app *appv1alpha1.App
		cm  *corev1.ConfigMap
	)

	BeforeEach(func() {
		serverName := testAppName
		nginxConfig := appv1alpha1.NginxConfig{
			ServerName: &serverName,
		}
		image, replicas, port := testImage, 1, testPort

		app = &appv1alpha1.App{
			ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: testNamespace},
			Spec: appv1alpha1.AppSpec{
				Image:       &image,
				Replicas:    &replicas,
				Port:        &port,
				NginxConfig: &nginxConfig,
			},
		}
		cm = desiredNginxConfConfigMap(app)
	})

	It("creates the expected ConfigMap", func() {
		Expect(cm.Name).To(Equal("demo-nginx-conf"))
		Expect(cm.Namespace).To(Equal(testNamespace))
		Expect(cm.Data).To(HaveLen(1))
		Expect(cm.Data["default.conf"]).To(ContainSubstring("server_name demo;"))
		Expect(cm.Data["default.conf"]).To(ContainSubstring("listen 80;"))
	})
})

var _ = Describe("nginxConfDrift", func() {
	var (
		app *appv1alpha1.App
		cm  *corev1.ConfigMap
	)

	BeforeEach(func() {
		image, replicas, port := testImage, 1, testPort

		app = &appv1alpha1.App{
			ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: testNamespace},
			Spec: appv1alpha1.AppSpec{
				Image:    &image,
				Replicas: &replicas,
				Port:     &port,
			},
		}

		// Build from the same constructor the controller uses, so the baseline cannot
		// drift away from the implementation.
		cm = desiredNginxConfConfigMap(app)
	})

	It("reports no drift for a freshly built ConfigMap", func() {
		Expect(nginxConfDrift(app, cm)).To(BeEmpty())
	})

	DescribeTable("reports exactly one field of drift",
		func(mutate func(*corev1.ConfigMap), want string) {
			mutate(cm)

			drift := nginxConfDrift(app, cm)
			Expect(drift).To(HaveLen(1), "drift: %v", drift)
			Expect(drift[0]).To(ContainSubstring(want))
		},
		Entry("config content changed", func(c *corev1.ConfigMap) {
			c.Data["default.conf"] = testDriftedConf
		}, `data["default.conf"]`),
		Entry("config key removed", func(c *corev1.ConfigMap) {
			delete(c.Data, "default.conf")
		}, `data["default.conf"]`),
		Entry("unexpected config key", func(c *corev1.ConfigMap) {
			c.Data["extra.conf"] = "server {}"
		}, `data["extra.conf"]`),
		Entry("label added", func(c *corev1.ConfigMap) {
			c.Labels[testLabelKey] = testLabelValue
		}, "metadata.labels"),
	)

	It("reports every drifted field at once", func() {
		cm.Data["default.conf"] = testDriftedConf
		cm.Data["extra.conf"] = "server {}"
		cm.Labels[testLabelKey] = testLabelValue

		Expect(nginxConfDrift(app, cm)).To(HaveLen(3))
	})
})
