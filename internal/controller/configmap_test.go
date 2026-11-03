package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appv1alpha1 "github.com/nikbonk/understanding-operators/api/v1alpha1"
)

// simple helper for cleaner tests
func strPtr(s string) *string { return &s }

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
		Entry("server name set, body size ommited",
			&appv1alpha1.NginxConfig{ServerName: strPtr("example.com")}, "example.com", "1m",
		),
		Entry("server ommited, body size set",
			&appv1alpha1.NginxConfig{ClientMaxBodySize: strPtr("5m")}, "_", "5m",
		),
		Entry("server name explicitly empty",
			&appv1alpha1.NginxConfig{ServerName: strPtr("")}, "_", "1m",
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
