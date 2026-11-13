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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
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

func nginxConfConfigMapName(app *appv1alpha1.App) string {
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

func desiredNginxConfConfigMap(app *appv1alpha1.App) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nginxConfConfigMapName(app),
			Namespace: app.Namespace,
			Labels:    labelsFor(app),
		},
		Data: map[string]string{
			"default.conf": renderNginxConfig(effectiveNginxConfig(app.Spec.NginxConfig)),
		},
	}
}

func nginxConfDrift(app *appv1alpha1.App, cm *corev1.ConfigMap) []string {
	var drift []string

	expectedConfigMap := desiredNginxConfConfigMap(app)

	if !maps.Equal(cm.Labels, expectedConfigMap.Labels) {
		drift = append(drift, fmt.Sprintf("metadata.labels: expected %v, found %v", expectedConfigMap.Labels, cm.Labels))
	}

	for key, want := range expectedConfigMap.Data {
		got, ok := cm.Data[key]
		switch {
		case !ok:
			drift = append(drift, fmt.Sprintf("data[%q]: missing", key))
		case got != want:
			drift = append(drift, fmt.Sprintf("data[%q]: differs", key))
		}
	}

	for key := range cm.Data {
		if _, ok := expectedConfigMap.Data[key]; !ok {
			drift = append(drift, fmt.Sprintf("data[%q]: unexpected", key))
		}
	}

	return drift

}

func (r *AppReconciler) ensureNginxConfConfigMap(ctx context.Context, app *appv1alpha1.App) (bool, error) {
	log := logf.FromContext(ctx)
	var cm corev1.ConfigMap
	reqCm := types.NamespacedName{Namespace: app.Namespace, Name: nginxConfConfigMapName(app)}

	err := r.Get(ctx, reqCm, &cm)
	if apierrors.IsNotFound(err) {
		log.Info("ConfigMap not found, will now be created")
		desired := desiredNginxConfConfigMap(app)

		if err := controllerutil.SetControllerReference(app, desired, r.Scheme); err != nil {
			log.Error(err, "Failed to set owner on Resource")
			return false, err
		}

		if err := r.Create(ctx, desired); err != nil {
			log.Error(err, "Failed to create ConfigMap")
			return false, err
		}

		log.Info("Created desired ConfigMap", "name", desired.Name)
		r.Recorder.Eventf(desired, nil, corev1.EventTypeNormal, "Created", "CreateConfigMap", "Created ConfigMap %s", desired.Name)
		r.Recorder.Eventf(app, nil, corev1.EventTypeNormal, "ConfigMapCreated", "CreateConfigMap", "Created ConfigMap %s", desired.Name)

		return true, nil
	}

	if err != nil {
		log.Error(err, "Failed to get ConfigMap resource")
		return false, err
	}

	drift := nginxConfDrift(app, &cm)
	if len(drift) != 0 {
		desired := desiredNginxConfConfigMap(app)
		cm.Data = desired.Data
		cm.Labels = desired.Labels

		if err := r.Update(ctx, &cm); err != nil {
			log.Error(err, "Failed to reconcile ConfigMap")
			return false, err
		}

		msg := strings.Join(drift, "; ")
		log.Info("Corrected ConfigMap drift", "diffs", drift)
		r.Recorder.Eventf(&cm, nil, corev1.EventTypeNormal, "Updated", "UpdateConfigMap", "Updated ConfigMap %s: %s", cm.Name, msg)
		r.Recorder.Eventf(app, nil, corev1.EventTypeNormal, "ConfigMapUpdated", "UpdateConfigMap", "ConfigMap drift corrected: %s", msg)

		return true, nil
	}

	return false, nil

}
