package controllers

import (
	"context"
	"fmt"

	controlplanev1alpha1 "github.com/loft-sh/cluster-api-provider-vcluster/api/controlplane/v1alpha1"
	infrastructurev1alpha1 "github.com/loft-sh/cluster-api-provider-vcluster/api/infrastructure/v1alpha1"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/patch"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	external "sigs.k8s.io/cluster-api/controllers/external"
)

var (
	skipInfrastructureClusterAnnotation = "vcluster.loft.sh/skip-infrastructure-cluster-patch"
)

func (r *GenericReconciler) patchInfrastructureCluster(ctx context.Context, vCluster GenericVCluster) (retErr error) {
	if !r.clusterKindExists || vCluster.GetAnnotations()[skipInfrastructureClusterAnnotation] == "true" {
		return nil
	}

	// get owner reference
	ownerRef := metav1.GetControllerOf(vCluster)
	if ownerRef == nil {
		return fmt.Errorf("no controller owner reference found")
	}

	// get the parent cluster
	parentCluster, err := external.Get(ctx, r.Client, &corev1.ObjectReference{
		APIVersion: ownerRef.APIVersion,
		Kind:       ownerRef.Kind,
		Name:       ownerRef.Name,
		Namespace:  vCluster.GetNamespace(),
	})
	if err != nil {
		return fmt.Errorf("failed to get parent cluster: %w", err)
	}

	// find infrastructure cluster
	apiVersion, _, err := unstructured.NestedString(parentCluster.Object, "spec", "infrastructureRef", "apiVersion")
	if err != nil {
		return fmt.Errorf("failed to get infrastructure cluster api version: %w", err)
	}
	kind, _, err := unstructured.NestedString(parentCluster.Object, "spec", "infrastructureRef", "kind")
	if err != nil {
		return fmt.Errorf("failed to get infrastructure cluster kind: %w", err)
	}

	// check if the infrastructure cluster is a vcluster, if yes then we don't need to patch the infrastructure cluster
	if kind == "VCluster" && (apiVersion == infrastructurev1alpha1.GroupVersion.String() || apiVersion == controlplanev1alpha1.GroupVersion.String()) {
		return nil
	}

	// set the external managed control plane to true to let Cluster API know that the control plane is externally managed
	vCluster.GetStatus().ExternalManagedControlPlane = ptr.To(true)

	// get the infrastructure provider name
	name, _, err := unstructured.NestedString(parentCluster.Object, "spec", "infrastructureRef", "name")
	if err != nil {
		return fmt.Errorf("failed to get infrastructure cluster name: %w", err)
	}

	// get the infrastructure cluster
	infrastructureCluster, err := external.Get(ctx, r.Client, &corev1.ObjectReference{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  vCluster.GetNamespace(),
	})
	if err != nil {
		return fmt.Errorf("failed to get infrastructure cluster: %w", err)
	}

	// create patch helper to persist changes
	patchHelper, err := patch.NewHelper(infrastructureCluster, r.Client)
	if err != nil {
		return fmt.Errorf("failed to create patch helper: %w", err)
	}
	defer func() {
		// when no error, patch the infrastructure cluster
		if retErr == nil {
			retErr = patchHelper.Patch(ctx, infrastructureCluster)
		}
	}()

	// we need to patch the spec.controlPlaneEndpoint for all providers
	switch kind {
	case "OpenStackCluster":
		// OpenStack does things differently, so we need to make an exception here and set spec.apiServerFixedIP and spec.apiServerPort
		if err = unstructured.SetNestedField(infrastructureCluster.Object, vCluster.GetSpec().ControlPlaneEndpoint.Host, "spec", "apiServerFixedIP"); err != nil {
			return fmt.Errorf("unable to patch api server fixed ip for %s: %w", infrastructureCluster.GetKind(), err)
		}
		if err = unstructured.SetNestedField(infrastructureCluster.Object, int64(vCluster.GetSpec().ControlPlaneEndpoint.Port), "spec", "apiServerPort"); err != nil {
			return fmt.Errorf("unable to patch api server port for %s: %w", infrastructureCluster.GetKind(), err)
		}
	default:
		// for all other providers we can just set spec.controlPlaneEndpoint
		if err = unstructured.SetNestedMap(infrastructureCluster.Object, map[string]interface{}{
			"host": vCluster.GetSpec().ControlPlaneEndpoint.Host,
			"port": int64(vCluster.GetSpec().ControlPlaneEndpoint.Port),
		}, "spec", "controlPlaneEndpoint"); err != nil {
			return fmt.Errorf("unable to patch control plane endpoint for %s: %w", infrastructureCluster.GetKind(), err)
		}
	}

	// some providers require us to set the status.ready field to true, so patch that here as well
	switch kind {
	case "KubevirtCluster", "NutanixCluster", "PacketCluster":
		if err = unstructured.SetNestedField(infrastructureCluster.Object, true, "status", "ready"); err != nil {
			return fmt.Errorf("unable to patch status.ready for %s: %w", infrastructureCluster.GetKind(), err)
		}
	}

	return nil
}
