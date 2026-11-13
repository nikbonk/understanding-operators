package controller

import (
	appv1alpha1 "github.com/nikbonk/understanding-operators/api/v1alpha1"
)

// Constants in this file describe the nginx workload as a whole rather than one
// object. They are read by more than one builder, which is what makes them a
// contract: change a value here and the Service, ConfigMap and Deployment must
// still agree.
const (
	labelInstance = "app.kubernetes.io/instance"
	labelName     = "app.kubernetes.io/name"

	// nginxContainerName is how the container is identified in the pod spec. Drift
	// comparison looks it up by name, because pod specs are lists and index order is
	// not something the API promises.
	nginxContainerName     = "nginx"
	nginxContainerPortName = "http"

	// nginxContainerPort is where nginx listens inside the pod. It is deliberately
	// not derived from Spec.Port: Spec.Port is the port users dial on the Service,
	// this is an implementation detail of the container. Three things must agree on
	// it - the ConfigMap's listen directive, the Service's TargetPort, and the
	// Deployment's containerPort.
	nginxContainerPort int32 = 80

	// nginxConfVolume is the name shared by the pod's Volume and the container's
	// VolumeMount. That shared name is the only link between them.
	nginxConfVolume    = "nginx-conf"
	nginxConfMountPath = "/etc/nginx/conf.d"
)

// labelsFor returns the labels shared by everything managed on behalf of app.
// This map is the contract between the Service's Spec.Selector and the
// Deployment's pod template labels. If the two ever diverge, the Service gets no
// Endpoints and nothing reports an error, so both callers must read it from here.
func labelsFor(app *appv1alpha1.App) map[string]string {
	return map[string]string{
		labelInstance: app.Name,
		labelName:     "nginx",
	}
}
