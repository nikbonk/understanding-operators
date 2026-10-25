package controller

import (
	appv1alpha1 "github.com/nikbonk/understanding-operators/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// service labels
const (
	labelInstance = "app.kubernetes.io/instance"
	labelName     = "app.kubernetes.io/name"
)

func labelsFor(app *appv1alpha1.App) map[string]string {
	return map[string]string{
		labelInstance: app.Name,
		labelName:     "nginx",
	}
}

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
