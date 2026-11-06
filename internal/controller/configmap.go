package controller

import (
	"fmt"

	appv1alpha1 "github.com/nikbonk/understanding-operators/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	nginxContainerPort      int32  = 80
	nginxDefaultServerName  string = "_"
	nginxDefaultMaxBodySize string = "1m"
	nginxConfTemplate              = `server {
	listen %d;
	server_name %s;

	root /usr/share/nginx/html;
	index index.html;

	client_max_body_size %s;

	location / {
		try_files $uri $uri/ =404;
	}
}
`
)

type nginxConfValues struct {
	ServerName        string
	ClientMaxBodySize string
}

func renderNginxConfig(cfg nginxConfValues) string {
	return fmt.Sprintf(nginxConfTemplate, nginxContainerPort, cfg.ServerName, cfg.ClientMaxBodySize)
}

func nginxConfigMapName(app *appv1alpha1.App) string {
	return app.Name + "-nginx-conf"
}

func effectiveNginxConfig(cfg *appv1alpha1.NginxConfig) nginxConfValues {
	v := nginxConfValues{
		ServerName:        nginxDefaultServerName,
		ClientMaxBodySize: nginxDefaultMaxBodySize,
	}

	if cfg == nil {
		return v
	}

	if cfg.ServerName != nil && *cfg.ServerName != "" {
		v.ServerName = *cfg.ServerName
	}

	if cfg.ClientMaxBodySize != nil && *cfg.ClientMaxBodySize != "" {
		v.ClientMaxBodySize = *cfg.ClientMaxBodySize
	}

	return v
}

func desiredNginxConfigMap(app *appv1alpha1.App) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nginxConfigMapName(app),
			Namespace: app.Namespace,
			Labels:    labelsFor(app),
		},
		Data: map[string]string{
			"default.conf": renderNginxConfig(effectiveNginxConfig(app.Spec.NginxConfig)),
		},
	}
}
