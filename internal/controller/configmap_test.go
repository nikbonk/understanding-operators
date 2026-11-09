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

var _ = Describe("desiredNginxConfigMap", func() {
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
			ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"},
			Spec: appv1alpha1.AppSpec{
				Image:       &image,
				Replicas:    &replicas,
				Port:        &port,
				NginxConfig: &nginxConfig,
			},
		}
		cm = desiredNginxConfigMap(app)
	})

	It("creates the expected ConfigMap", func() {
		Expect(cm.Name).To(Equal("demo-nginx-conf"))
		Expect(cm.Namespace).To(Equal("default"))
		Expect(cm.Data).To(HaveLen(1))
		Expect(cm.Data["default.conf"]).To(ContainSubstring("server_name demo;"))
		Expect(cm.Data["default.conf"]).To(ContainSubstring("listen 80;"))
	})
})
