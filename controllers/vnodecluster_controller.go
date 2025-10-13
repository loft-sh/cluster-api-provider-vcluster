/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/go-logr/logr"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/conditions"
	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	infrav1alpha1 "github.com/loft-sh/cluster-api-provider-vcluster/api/infrastructure/v1alpha1"
	klog "k8s.io/klog/v2"
)

// VNodeClusterReconciler reconciles a VNodeCluster object.
type VNodeClusterReconciler struct {
	client.Client
	Log logr.Logger
}

// Reconcile reads that state of the cluster for a KubevirtCluster object and makes changes based on the state read
// and what is in the KubevirtCluster.Spec.
func (r *VNodeClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := r.Log.WithValues("cluster", req.NamespacedName)

	// Fetch the VNodeCluster.
	vNodeCluster := &infrav1alpha1.VNodeCluster{}
	if err := r.Get(ctx, req.NamespacedName, vNodeCluster); err != nil {
		if kerrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Cluster.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, vNodeCluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info("Waiting for Cluster Controller to set OwnerRef on KubevirtCluster")
		return ctrl.Result{}, nil
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(vNodeCluster, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	// Always attempt to Patch the KubevirtCluster object and status after each reconciliation.
	defer func() {
		if err := r.PatchVNodeCluster(ctx, patchHelper, vNodeCluster); err != nil {
			if err = filterOutNotFoundError(err); err != nil {
				klog.Error(err, "failed to patch VNodeCluster")
				if rerr == nil {
					rerr = err
				}
			}
		}
	}()

	// Handle deleted clusters
	if !vNodeCluster.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(vNodeCluster, cluster)
}

func (r *VNodeClusterReconciler) reconcileNormal(vNodeCluster *infrav1alpha1.VNodeCluster, regularCluster *clusterv1.Cluster) (ctrl.Result, error) {
	// Get the ControlPlane Host and Port manually set by the user if existing
	if regularCluster.Spec.ControlPlaneEndpoint.Host != "" {
		vNodeCluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
			Host: vNodeCluster.Spec.ControlPlaneEndpoint.Host,
			Port: vNodeCluster.Spec.ControlPlaneEndpoint.Port,
		}
	}

	// Mark the vNodeCluster ready
	vNodeCluster.Status.Ready = true
	return ctrl.Result{}, nil
}

// SetupWithManager will add watches for this controller.
func (r *VNodeClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1alpha1.VNodeCluster{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPaused(r.Scheme(), ctrl.LoggerFrom(ctx))).
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(util.ClusterToInfrastructureMapFunc(
				ctx,
				infrav1alpha1.GroupVersion.WithKind("VNodeCluster"),
				mgr.GetClient(),
				&infrav1alpha1.VNodeCluster{},
			)),
			builder.WithPredicates(predicates.ClusterUnpaused(r.Scheme(), ctrl.LoggerFrom(ctx))),
		).
		Complete(r)
}

// PatchVNodeCluster patches the VNodeCluster object and status.
func (r *VNodeClusterReconciler) PatchVNodeCluster(ctx context.Context, patchHelper *patch.Helper, vNodeCluster *infrav1alpha1.VNodeCluster) error {
	// Always update the readyCondition by summarizing the state of other conditions.
	// A step counter is added to represent progress during the provisioning process (instead we are hiding it during the deletion process).
	conditions.SetSummary(vNodeCluster, conditions.WithConditions(infrav1alpha1.ReadyCondition))

	// Patch the object, ignoring conflicts on the conditions owned by this controller.
	return patchHelper.Patch(
		ctx,
		vNodeCluster,
		patch.WithOwnedConditions{Conditions: []string{
			string(infrav1alpha1.ReadyCondition),
		}},
	)
}

func filterOutNotFoundError(err error) error {
	if err == nil {
		return nil
	}
	var aggErr utilerrors.Aggregate
	if errors.As(err, &aggErr) {
		var errList []error
		for _, err := range aggErr.Errors() {
			if !kerrors.IsNotFound(err) {
				errList = append(errList, err)
			}
		}
		return utilerrors.NewAggregate(errList)
	}
	if !kerrors.IsNotFound(err) {
		return err
	}
	return nil
}
