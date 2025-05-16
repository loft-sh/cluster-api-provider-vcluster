/*
Copyright 2022.

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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrastructurev1alpha1 "github.com/loft-sh/cluster-api-provider-vcluster/api/infrastructure/v1alpha1"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/constants"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/helm"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/conditions"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/kubeconfighelper"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/patch"
)

// GenericReconciler reconciles a VCluster object
type GenericReconciler struct {
	Client             client.Client
	HelmClient         helm.Client
	HelmSecrets        *helm.Secrets
	Log                logr.Logger
	Scheme             *runtime.Scheme
	ClientConfigGetter ClientConfigGetter
	HTTPClientGetter   HTTPClientGetter
	ControllerName     string
	clusterKindExists  bool

	New func() GenericVCluster
}

type GenericVCluster interface {
	conditions.Setter

	GetSpec() *infrastructurev1alpha1.VClusterSpec
	GetStatus() *infrastructurev1alpha1.VClusterStatus
}

func (r *GenericReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	r.Log.Info("Reconcile vCluster", "namespacedName", req.NamespacedName)

	// get virtual cluster object
	vCluster := r.New()
	err := r.Client.Get(ctx, req.NamespacedName, vCluster)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// is deleting?
	if vCluster.GetDeletionTimestamp() != nil {
		// check if namespace is deleting
		namespace := &corev1.Namespace{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: req.Namespace}, namespace)
		if err != nil {
			return ctrl.Result{}, nil
		} else if namespace.DeletionTimestamp != nil {
			return ctrl.Result{}, RemoveFinalizer(ctx, r.Client, vCluster, CleanupFinalizer)
		}

		err = deleteHelmChart(ctx, r.HelmClient, r.HelmSecrets, req.Namespace, req.Name, r.Log)
		if err != nil {
			return ctrl.Result{}, err
		}

		// delete the persistent volume claim
		err = r.Client.Delete(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data-" + vCluster.GetName() + "-0", Namespace: req.Namespace}})
		if err != nil && !kerrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, RemoveFinalizer(ctx, r.Client, vCluster, CleanupFinalizer)
	}

	// is there an owner Cluster CR set by CAPI cluster controller?
	// only check when installed via CAPI - Cluster CRD is present
	if r.clusterKindExists {
		clusterOwner := false
		for _, v := range vCluster.GetOwnerReferences() {
			if v.Kind == "Cluster" {
				clusterOwner = true
				break
			}
		}
		if !clusterOwner {
			// as per CAPI docs:
			// The cluster controller will set an OwnerReference on the infrastructureCluster.
			// This controller should normally take no action during reconciliation until it sees the OwnerReference.
			return ctrl.Result{}, nil
		}
	}

	// ensure finalizer
	err = EnsureFinalizer(ctx, r.Client, vCluster, CleanupFinalizer)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(vCluster, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer func() {
		// Always reconcile the Status.Phase field.
		r.reconcilePhase(vCluster)

		// Always attempt to Patch the Cluster object and status after each reconciliation.
		// Patch ObservedGeneration only if the reconciliation completed successfully
		patchOpts := []patch.Option{}
		if reterr == nil {
			patchOpts = append(patchOpts, patch.WithStatusObservedGeneration{})
		}
		if err := patchCluster(ctx, patchHelper, vCluster, patchOpts...); err != nil {
			reterr = utilerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// check if we have to redeploy
	err = r.redeployIfNeeded(ctx, vCluster)
	if err != nil {
		r.Log.Error(err, "error during virtual cluster deploy",
			"namespace", vCluster.GetNamespace(),
			"name", vCluster.GetName(),
		)
		conditions.MarkFalse(vCluster, infrastructurev1alpha1.HelmChartDeployedCondition, "HelmDeployFailed", infrastructurev1alpha1.ConditionSeverityError, "%v", err)
		return ctrl.Result{RequeueAfter: time.Second * 5}, err
	}

	// check if vcluster is initialized and sync the kubeconfig Secret
	restConfig, err := r.syncVClusterKubeconfig(ctx, vCluster)
	if err != nil {
		r.Log.Info("vCluster is not ready",
			"namespace", vCluster.GetNamespace(),
			"name", vCluster.GetName(),
			"err", err,
		)
		conditions.MarkFalse(vCluster, infrastructurev1alpha1.KubeconfigReadyCondition, "CheckFailed", infrastructurev1alpha1.ConditionSeverityWarning, "%v", err)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	// sync the infrastructure cluster
	err = r.patchInfrastructureCluster(ctx, vCluster)
	if err != nil {
		r.Log.Error(err, "error during infrastructure cluster patch",
			"namespace", vCluster.GetNamespace(),
			"name", vCluster.GetName(),
		)
		conditions.MarkFalse(vCluster, infrastructurev1alpha1.InfrastructureClusterSyncedCondition, "PatchFailed", infrastructurev1alpha1.ConditionSeverityError, "%v", err)
		return ctrl.Result{}, err
	}
	conditions.MarkTrue(vCluster, infrastructurev1alpha1.InfrastructureClusterSyncedCondition)

	// check if vcluster is ready
	vCluster.GetStatus().Ready, err = r.checkReadyz(vCluster, restConfig)
	if err != nil || !vCluster.GetStatus().Ready {
		r.Log.V(1).Info("readiness check failed", "err", err)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *GenericReconciler) reconcilePhase(vCluster GenericVCluster) {
	logger := r.Log
	oldPhase := vCluster.GetStatus().Phase

	// Check for failed state first
	for _, condition := range vCluster.GetStatus().Conditions {
		if condition.Status == corev1.ConditionFalse && condition.Severity == infrastructurev1alpha1.ConditionSeverityError {
			vCluster.GetStatus().Phase = infrastructurev1alpha1.VirtualClusterFailed
			vCluster.GetStatus().Reason = condition.Reason
			vCluster.GetStatus().Message = condition.Message
			break
		}
	}

	// If not failed, check if deployed or pending
	if vCluster.GetStatus().Phase != infrastructurev1alpha1.VirtualClusterFailed {
		if vCluster.GetStatus().Ready && conditions.IsTrue(vCluster, infrastructurev1alpha1.ControlPlaneInitializedCondition) {
			vCluster.GetStatus().Phase = infrastructurev1alpha1.VirtualClusterDeployed
		} else {
			vCluster.GetStatus().Phase = infrastructurev1alpha1.VirtualClusterPending
		}
	}

	// Log phase transitions
	if oldPhase != vCluster.GetStatus().Phase {
		logger.Info("vcluster phase changed",
			"namespace", vCluster.GetNamespace(),
			"name", vCluster.GetName(),
			"oldPhase", oldPhase,
			"newPhase", vCluster.GetStatus().Phase,
			"reason", vCluster.GetStatus().Reason,
			"message", vCluster.GetStatus().Message,
		)
	}
}

func (r *GenericReconciler) redeployIfNeeded(_ context.Context, vCluster GenericVCluster) error {
	if vCluster.GetGeneration() == vCluster.GetStatus().ObservedGeneration &&
		conditions.IsTrue(vCluster, infrastructurev1alpha1.HelmChartDeployedCondition) {
		return nil
	}

	logger := r.Log

	chartRepo := constants.DefaultVClusterRepo
	if vCluster.GetSpec().HelmRelease != nil && vCluster.GetSpec().HelmRelease.Chart.Repo != "" {
		chartRepo = vCluster.GetSpec().HelmRelease.Chart.Repo
	}

	chartName := constants.DefaultVClusterChartName
	if vCluster.GetSpec().HelmRelease != nil && vCluster.GetSpec().HelmRelease.Chart.Name != "" {
		chartName = vCluster.GetSpec().HelmRelease.Chart.Name
	}

	var chartVersion string
	if vCluster.GetSpec().HelmRelease != nil && vCluster.GetSpec().HelmRelease.Chart.Version != "" {
		chartVersion = vCluster.GetSpec().HelmRelease.Chart.Version
		// Remove 'v' prefix if present
		if len(chartVersion) > 0 && chartVersion[0] == 'v' {
			chartVersion = chartVersion[1:]
		}
	}

	var values string
	if vCluster.GetSpec().HelmRelease != nil {
		if vCluster.GetSpec().HelmRelease.Values != "" && vCluster.GetSpec().HelmRelease.ValuesObject != nil {
			return fmt.Errorf("both values and valuesObject cannot be set")
		}

		if vCluster.GetSpec().HelmRelease.ValuesObject != nil {
			rawValues, err := json.Marshal(vCluster.GetSpec().HelmRelease.ValuesObject)
			if err != nil {
				return fmt.Errorf("error marshalling valuesObject: %w", err)
			}

			values = string(rawValues)
		} else if vCluster.GetSpec().HelmRelease.Values != "" {
			values = vCluster.GetSpec().HelmRelease.Values
		}
	}

	if !conditions.IsTrue(vCluster, infrastructurev1alpha1.HelmChartDeployedCondition) {
		logger.Info("deploying vcluster",
			"namespace", vCluster.GetNamespace(),
			"clusterName", vCluster.GetName(),
			"chartRepo", chartRepo,
			"chartName", chartName,
			"chartVersion", chartVersion,
			"values", values,
		)
	} else {
		logger.Info("upgrading vcluster",
			"namespace", vCluster.GetNamespace(),
			"clusterName", vCluster.GetName(),
			"chartRepo", chartRepo,
			"chartName", chartName,
			"chartVersion", chartVersion,
			"values", values,
		)
	}

	var chartPath string
	if chartVersion != "" {
		chartPath = fmt.Sprintf("./%s-%s.tgz", chartName, chartVersion)
	} else {
		chartPath = fmt.Sprintf("./%s-latest.tgz", chartName)
	}

	_, err := os.Stat(chartPath)
	if err != nil {
		// we have to upgrade / install the chart
		err = r.HelmClient.Upgrade(vCluster.GetName(), vCluster.GetNamespace(), helm.UpgradeOptions{
			Chart:   chartName,
			Repo:    chartRepo,
			Version: chartVersion,
			Values:  values,
		})
	} else {
		// we have to upgrade / install the chart
		err = r.HelmClient.Upgrade(vCluster.GetName(), vCluster.GetNamespace(), helm.UpgradeOptions{
			Path:   chartPath,
			Values: values,
		})
	}
	if err != nil {
		if len(err.Error()) > 512 {
			err = fmt.Errorf("%v ... ", err.Error()[:512])
		}

		return fmt.Errorf("error installing / upgrading vcluster: %w", err)
	}

	conditions.MarkTrue(vCluster, infrastructurev1alpha1.HelmChartDeployedCondition)
	conditions.Delete(vCluster, infrastructurev1alpha1.KubeconfigReadyCondition)

	return nil
}

func (r *GenericReconciler) syncVClusterKubeconfig(ctx context.Context, vCluster GenericVCluster) (*rest.Config, error) {
	credentials, err := GetVClusterCredentials(ctx, r.Client, vCluster)
	if err != nil {
		return nil, err
	}

	restConfig, err := kubeconfighelper.NewVClusterClientConfig(vCluster.GetName(), vCluster.GetNamespace(), "", credentials.ClientCert, credentials.ClientKey)
	if err != nil {
		return nil, err
	}

	kubeClient, err := r.ClientConfigGetter.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	ctxTimeout, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	// if we haven't checked if the vcluster is initialized, do it now
	if !conditions.IsTrue(vCluster, infrastructurev1alpha1.ControlPlaneInitializedCondition) {
		_, err = kubeClient.CoreV1().ServiceAccounts("default").Get(ctxTimeout, "default", metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		conditions.MarkTrue(vCluster, infrastructurev1alpha1.ControlPlaneInitializedCondition)
	}
	// setting .Status.Initialized outside of the condition above to ensure
	// that it is set on old CRs, which were missing this field, as well
	vCluster.GetStatus().Initialized = true

	// write kubeconfig to the vcluster.Name+"-kubeconfig" Secret as expected by CAPI convention
	kubeConfig, err := GetVClusterKubeConfig(ctx, r.Client, vCluster)
	if err != nil {
		return nil, fmt.Errorf("can not retrieve kubeconfig: %w", err)
	}
	if len(kubeConfig.Clusters) != 1 {
		return nil, fmt.Errorf("unexpected kube config")
	}

	// If vcluster.spec.controlPlaneEndpoint.Host is not set, try to autodiscover it from
	// the Service that targets vcluster pods, and write it back into the spec.
	controlPlaneHost := vCluster.GetSpec().ControlPlaneEndpoint.Host
	if controlPlaneHost == "" {
		controlPlaneHost, err = DiscoverHostFromService(ctx, r.Client, vCluster)
		if err != nil {
			return nil, err
		}
		// write the discovered host back into vCluster CR
		vCluster.GetSpec().ControlPlaneEndpoint.Host = controlPlaneHost
		if vCluster.GetSpec().ControlPlaneEndpoint.Port == 0 {
			vCluster.GetSpec().ControlPlaneEndpoint.Port = DefaultControlPlanePort
		}
	}

	for k := range kubeConfig.Clusters {
		host := kubeConfig.Clusters[k].Server
		if controlPlaneHost != "" {
			if vCluster.GetSpec().ControlPlaneEndpoint.Port != 0 {
				host = fmt.Sprintf("%s:%d", controlPlaneHost, vCluster.GetSpec().ControlPlaneEndpoint.Port)
			} else {
				host = fmt.Sprintf("%s:%d", controlPlaneHost, DefaultControlPlanePort)
			}
		}
		if !strings.HasPrefix(host, "https://") {
			host = "https://" + host
		}
		kubeConfig.Clusters[k].Server = host
	}
	outKubeConfig, err := clientcmd.Write(*kubeConfig)
	if err != nil {
		return nil, err
	}

	// get cluster name
	clusterName := vCluster.GetName()
	if r.clusterKindExists {
		for _, ownerRef := range vCluster.GetOwnerReferences() {
			if ownerRef.Kind == "Cluster" {
				clusterName = ownerRef.Name
				break
			}
		}
	}

	// create kubeconfig secret
	kubeSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-kubeconfig", clusterName),
			Namespace: vCluster.GetNamespace(),
			Labels: map[string]string{
				clusterv1beta1.ClusterNameLabel: clusterName,
			},
		},
	}
	_, err = controllerutil.CreateOrPatch(ctx, r.Client, kubeSecret, func() error {
		kubeSecret.Type = clusterv1beta1.ClusterSecretType
		kubeSecret.Data = map[string][]byte{
			KubeconfigDataName: outKubeConfig,
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("can not create a kubeconfig secret: %w", err)
	}

	// create ca secret
	certsSecret := &corev1.Secret{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-certs", vCluster.GetName()), Namespace: vCluster.GetNamespace()}, certsSecret)
	if err != nil {
		return nil, fmt.Errorf("can not get certs secret: %w", err)
	}
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ca", clusterName),
			Namespace: vCluster.GetNamespace(),
			Labels: map[string]string{
				clusterv1beta1.ClusterNameLabel: clusterName,
			},
		},
	}
	_, err = controllerutil.CreateOrPatch(ctx, r.Client, caSecret, func() error {
		caSecret.Data = map[string][]byte{
			TLSKeyDataName: certsSecret.Data["ca.key"],
			TLSCrtDataName: certsSecret.Data["ca.crt"],
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("can not create a kubeconfig secret: %w", err)
	}

	conditions.MarkTrue(vCluster, infrastructurev1alpha1.KubeconfigReadyCondition)
	return restConfig, nil
}

func (r *GenericReconciler) checkReadyz(vCluster GenericVCluster, restConfig *rest.Config) (bool, error) {
	t := time.Now()
	transport, err := rest.TransportFor(restConfig)
	if err != nil {
		return false, err
	}
	client := r.HTTPClientGetter.ClientFor(transport, 10*time.Second)
	resp, err := client.Get(fmt.Sprintf("https://%s:%d/readyz", vCluster.GetSpec().ControlPlaneEndpoint.Host, vCluster.GetSpec().ControlPlaneEndpoint.Port))
	r.Log.V(1).Info("ready check done", "namespace", vCluster.GetNamespace(), "name", vCluster.GetName(), "duration", time.Since(t))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if string(body) != "ok" {
		return false, nil
	}

	return true, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GenericReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error
	r.clusterKindExists, err = kindExists(mgr.GetConfig(), clusterv1beta1.GroupVersion.WithKind("Cluster"))
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(r.New()).
		Named(r.ControllerName).
		Complete(r)
}
