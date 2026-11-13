package controller

import (
	"fmt"
	"maps"

	appv1alpha1 "github.com/nikbonk/understanding-operators/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func desiredDeployment(app *appv1alpha1.App) *appsv1.Deployment {
	// less calls
	labels := labelsFor(app)

	containers := []corev1.Container{{
		Name:  nginxContainerName,
		Image: *app.Spec.Image,
		Ports: []corev1.ContainerPort{{
			Name:          nginxContainerPortName,
			ContainerPort: nginxContainerPort,
			Protocol:      corev1.ProtocolTCP,
		}},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      nginxConfVolume,
			ReadOnly:  true,
			MountPath: nginxConfMountPath,
		}},
	}}

	volumes := []corev1.Volume{{
		Name: nginxConfVolume,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: nginxConfConfigMapName(app),
				},
			},
		},
	}}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(*app.Spec.Replicas)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: containers,
					Volumes:    volumes,
				},
			},
		},
	}
}

func deploymentDrift(app *appv1alpha1.App, deploy *appsv1.Deployment) []string {
	var drift []string

	expectedDeployment := desiredDeployment(app)
	want := expectedDeployment.Spec
	got := deploy.Spec

	if !maps.Equal(deploy.Labels, expectedDeployment.Labels) {
		drift = append(drift, fmt.Sprintf("metadata.labels: expected %v, found %v", expectedDeployment.Labels, deploy.Labels))
	}

	// The API server defaults replicas on create, so it is non-nil for any object
	// that came back from the API server.
	if got.Replicas != nil && want.Replicas != nil && *got.Replicas != *want.Replicas {
		drift = append(drift, fmt.Sprintf("spec.replicas: expected %d, found %d", *want.Replicas, *got.Replicas))
	}

	if !maps.Equal(got.Template.Labels, want.Template.Labels) {
		drift = append(drift, fmt.Sprintf("spec.template.labels: expected %v, found %v", want.Template.Labels, got.Template.Labels))
	}

	// Containers are a list, so the count is part of the contract before any field
	// inside one can be compared. Indexing [0] is only safe in the default branch.
	switch {
	case len(got.Template.Spec.Containers) == 0:
		drift = append(drift, "spec.template.spec.containers: expected 1 container, found none")
	case len(got.Template.Spec.Containers) != len(want.Template.Spec.Containers):
		drift = append(drift, fmt.Sprintf("spec.template.spec.containers: expected %d container(s), found %d",
			len(want.Template.Spec.Containers), len(got.Template.Spec.Containers)))
	default:
		foundContainer := got.Template.Spec.Containers[0]
		wantContainer := want.Template.Spec.Containers[0]

		if foundContainer.Name != wantContainer.Name {
			drift = append(drift, fmt.Sprintf("spec.template.spec.containers[0].name: expected %q, found %q",
				wantContainer.Name, foundContainer.Name))
		}

		if foundContainer.Image != wantContainer.Image {
			drift = append(drift, fmt.Sprintf("spec.template.spec.containers[0].image: expected %q, found %q",
				wantContainer.Image, foundContainer.Image))
		}

		switch {
		case len(foundContainer.Ports) != len(wantContainer.Ports):
			drift = append(drift, fmt.Sprintf("spec.template.spec.containers[0].ports: expected %d port(s), found %d",
				len(wantContainer.Ports), len(foundContainer.Ports)))
		default:
			for i, wantPort := range wantContainer.Ports {
				if foundContainer.Ports[i].ContainerPort != wantPort.ContainerPort {
					drift = append(drift, fmt.Sprintf("spec.template.spec.containers[0].ports[%d].containerPort: expected %d, found %d",
						i, wantPort.ContainerPort, foundContainer.Ports[i].ContainerPort))
				}
			}
		}

		switch {
		case len(foundContainer.VolumeMounts) != len(wantContainer.VolumeMounts):
			drift = append(drift, fmt.Sprintf("spec.template.spec.containers[0].volumeMounts: expected %d mount(s), found %d",
				len(wantContainer.VolumeMounts), len(foundContainer.VolumeMounts)))
		default:
			for i, wantMount := range wantContainer.VolumeMounts {
				foundMount := foundContainer.VolumeMounts[i]

				if foundMount.Name != wantMount.Name {
					drift = append(drift, fmt.Sprintf("spec.template.spec.containers[0].volumeMounts[%d].name: expected %q, found %q",
						i, wantMount.Name, foundMount.Name))
				}

				if foundMount.MountPath != wantMount.MountPath {
					drift = append(drift, fmt.Sprintf("spec.template.spec.containers[0].volumeMounts[%d].mountPath: expected %q, found %q",
						i, wantMount.MountPath, foundMount.MountPath))
				}
			}
		}
	}

	switch {
	case len(got.Template.Spec.Volumes) != len(want.Template.Spec.Volumes):
		drift = append(drift, fmt.Sprintf("spec.template.spec.volumes: expected %d volume(s), found %d",
			len(want.Template.Spec.Volumes), len(got.Template.Spec.Volumes)))
	default:
		for i, wantVolume := range want.Template.Spec.Volumes {
			foundVolume := got.Template.Spec.Volumes[i]

			if foundVolume.Name != wantVolume.Name {
				drift = append(drift, fmt.Sprintf("spec.template.spec.volumes[%d].name: expected %q, found %q",
					i, wantVolume.Name, foundVolume.Name))
			}

			// ConfigMap is a pointer: a volume backed by anything else (Secret, PVC,
			// emptyDir) is drift even when the volume name matches.
			if wantVolume.ConfigMap == nil {
				continue
			}
			if foundVolume.ConfigMap == nil {
				drift = append(drift, fmt.Sprintf("spec.template.spec.volumes[%d].configMap: expected %q, found no configMap source",
					i, wantVolume.ConfigMap.Name))
				continue
			}
			if foundVolume.ConfigMap.Name != wantVolume.ConfigMap.Name {
				drift = append(drift, fmt.Sprintf("spec.template.spec.volumes[%d].configMap.name: expected %q, found %q",
					i, wantVolume.ConfigMap.Name, foundVolume.ConfigMap.Name))
			}
		}
	}

	return drift
}
