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
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1alpha1 "github.com/loft-sh/cluster-api-provider-vcluster/api/v1alpha1"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/constants"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/helm"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/conditions"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/kubeconfighelper"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/patch"
)

type ClientConfigGetter interface {
	NewForConfig(restConfig *rest.Config) (kubernetes.Interface, error)
}

type clientConfigGetter struct {
}

func (c *clientConfigGetter) NewForConfig(restConfig *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(restConfig)
}

func NewClientConfigGetter() ClientConfigGetter {
	return &clientConfigGetter{}
}

type HTTPClientGetter interface {
	ClientFor(r http.RoundTripper, timeout time.Duration) *http.Client
}

type httpClientGetter struct {
}

func (h *httpClientGetter) ClientFor(r http.RoundTripper, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: r,
	}
}

func NewHTTPClientGetter() HTTPClientGetter {
	return &httpClientGetter{}
}

// VClusterReconciler reconciles a VCluster object
type VClusterReconciler struct {
	client.Client
	HelmClient         helm.Client
	HelmSecrets        *helm.Secrets
	Log                logr.Logger
	Scheme             *runtime.Scheme
	ClientConfigGetter ClientConfigGetter
	HTTPClientGetter   HTTPClientGetter
	clusterKindExists  bool
}

type Credentials struct {
	ClientCert []byte
	ClientKey  []byte
}

const (
	// A finalizer that is added to the VCluster CR to ensure that helm delete is executed.
	CleanupFinalizer = "vcluster.loft.sh/cleanup"

	DefaultControlPlanePort = 443

	// KubeconfigDataName is the key used to store a Kubeconfig in the secret's data field.
	KubeconfigDataName = "value"
)

func (r *VClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	r.Log.V(1).Info("Reconcile", "namespacedName", req.NamespacedName)

	// get virtual cluster object
	vCluster := &v1alpha1.VCluster{}
	err := r.Client.Get(ctx, req.NamespacedName, vCluster)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// is deleting?
	if vCluster.DeletionTimestamp != nil {
		// check if namespace is deleting
		namespace := &corev1.Namespace{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: req.Namespace}, namespace)
		if err != nil {
			return ctrl.Result{}, nil
		} else if namespace.DeletionTimestamp != nil {
			return ctrl.Result{}, RemoveFinalizer(ctx, r.Client, vCluster, CleanupFinalizer)
		}

		err = r.deleteHelmChart(ctx, req.Namespace, req.Name)
		if err != nil {
			return ctrl.Result{}, err
		}

		// delete the persistent volume claim
		err = r.Client.Delete(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data-" + vCluster.Name + "-0", Namespace: req.Namespace}})
		if err != nil && !kerrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, RemoveFinalizer(ctx, r.Client, vCluster, CleanupFinalizer)
	}

	// is there an owner Cluster CR set by CAPI cluster controller?
	// only check when installed via CAPI - Cluster CRD is present
	if r.clusterKindExists {
		clusterOwner := false
		for _, v := range vCluster.OwnerReferences {
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
			"namespace", vCluster.Namespace,
			"name", vCluster.Name,
		)
		conditions.MarkFalse(vCluster, v1alpha1.HelmChartDeployedCondition, "HelmDeployFailed", v1alpha1.ConditionSeverityError, "%v", err)
		return ctrl.Result{RequeueAfter: time.Second * 5}, err
	}

	// check if vcluster is initialized and sync the kubeconfig Secret
	restConfig, err := r.syncVClusterKubeconfig(ctx, vCluster)
	if err != nil {
		r.Log.V(1).Info("vcluster is not ready",
			"namespace", vCluster.Namespace,
			"name", vCluster.Name,
			"err", err,
		)
		conditions.MarkFalse(vCluster, v1alpha1.KubeconfigReadyCondition, "CheckFailed", v1alpha1.ConditionSeverityWarning, "%v", err)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	vCluster.Status.Ready, err = r.checkReadyz(vCluster, restConfig)
	if err != nil || !vCluster.Status.Ready {
		r.Log.V(1).Info("readiness check failed", "err", err)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *VClusterReconciler) reconcilePhase(vCluster *v1alpha1.VCluster) {
	if vCluster.Status.Phase != v1alpha1.VirtualClusterPending {
		vCluster.Status.Phase = v1alpha1.VirtualClusterPending
	}

	if vCluster.Status.Ready && conditions.IsTrue(vCluster, v1alpha1.ControlPlaneInitializedCondition) {
		vCluster.Status.Phase = v1alpha1.VirtualClusterDeployed
	}

	// set failed if a condition is errored
	vCluster.Status.Reason = ""
	vCluster.Status.Message = ""
	for _, c := range vCluster.Status.Conditions {
		if c.Status == corev1.ConditionFalse && c.Severity == v1alpha1.ConditionSeverityError {
			vCluster.Status.Phase = v1alpha1.VirtualClusterFailed
			vCluster.Status.Reason = c.Reason
			vCluster.Status.Message = c.Message
			break
		}
	}
}

func (r *VClusterReconciler) redeployIfNeeded(_ context.Context, vCluster *v1alpha1.VCluster) error {
	// upgrade chart
	if vCluster.Generation == vCluster.Status.ObservedGeneration && conditions.IsTrue(vCluster, v1alpha1.HelmChartDeployedCondition) {
		return nil
	}

	r.Log.V(1).Info("upgrade virtual cluster helm chart",
		"namespace", vCluster.Namespace,
		"clusterName", vCluster.Name,
	)

	var chartRepo string
	if vCluster.Spec.HelmRelease != nil {
		chartRepo = vCluster.Spec.HelmRelease.Chart.Repo
	}
	if chartRepo == "" {
		chartRepo = constants.DefaultVClusterRepo
	}

	// chart name
	var chartName string
	if vCluster.Spec.HelmRelease != nil {
		chartName = vCluster.Spec.HelmRelease.Chart.Name
	}
	if chartName == "" {
		chartName = constants.DefaultVClusterChartName
	}

	if vCluster.Spec.HelmRelease == nil || vCluster.Spec.HelmRelease.Chart.Version == "" {
		return fmt.Errorf("empty value of the .spec.HelmRelease.Version field")
	}
	// chart version
	chartVersion := vCluster.Spec.HelmRelease.Chart.Version

	if len(chartVersion) > 0 && chartVersion[0] == 'v' {
		chartVersion = chartVersion[1:]
	}

	// determine values
	var values string
	if vCluster.Spec.HelmRelease != nil || vCluster.Spec.HelmRelease.Values == "" {
		values = vCluster.Spec.HelmRelease.Values
	}

	r.Log.Info("Deploy virtual cluster",
		"namespace", vCluster.Namespace,
		"clusterName", vCluster.Name,
		"values", values,
	)
	chartPath := "./" + chartName + "-" + chartVersion + ".tgz"
	_, err := os.Stat(chartPath)
	if err != nil {
		// we have to upgrade / install the chart
		err = r.HelmClient.Upgrade(vCluster.Name, vCluster.Namespace, helm.UpgradeOptions{
			Chart:   chartName,
			Repo:    chartRepo,
			Version: chartVersion,
			Values:  values,
		})
	} else {
		// we have to upgrade / install the chart
		err = r.HelmClient.Upgrade(vCluster.Name, vCluster.Namespace, helm.UpgradeOptions{
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

	conditions.MarkTrue(vCluster, v1alpha1.HelmChartDeployedCondition)
	conditions.Delete(vCluster, v1alpha1.KubeconfigReadyCondition)

	return nil
}

func (r *VClusterReconciler) syncVClusterKubeconfig(ctx context.Context, vCluster *v1alpha1.VCluster) (*rest.Config, error) {
	credentials, err := GetVClusterCredentials(ctx, r.Client, vCluster)
	if err != nil {
		return nil, err
	}

	restConfig, err := kubeconfighelper.NewVClusterClientConfig(vCluster.Name, vCluster.Namespace, "", credentials.ClientCert, credentials.ClientKey)
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
	if !conditions.IsTrue(vCluster, v1alpha1.ControlPlaneInitializedCondition) {
		_, err = kubeClient.CoreV1().ServiceAccounts("default").Get(ctxTimeout, "default", metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		conditions.MarkTrue(vCluster, v1alpha1.ControlPlaneInitializedCondition)
	}
	// setting .Status.Initialized outside of the condition above to ensure
	// that it is set on old CRs, which were missing this field, as well
	vCluster.Status.Initialized = true

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
	controlPlaneHost := vCluster.Spec.ControlPlaneEndpoint.Host
	if controlPlaneHost == "" {
		controlPlaneHost, err = DiscoverHostFromService(ctx, r.Client, vCluster)
		if err != nil {
			return nil, err
		}
		// write the discovered host back into vCluster CR
		vCluster.Spec.ControlPlaneEndpoint.Host = controlPlaneHost
		if vCluster.Spec.ControlPlaneEndpoint.Port == 0 {
			vCluster.Spec.ControlPlaneEndpoint.Port = DefaultControlPlanePort
		}
	}

	for k := range kubeConfig.Clusters {
		host := kubeConfig.Clusters[k].Server
		if controlPlaneHost != "" {
			if vCluster.Spec.ControlPlaneEndpoint.Port != 0 {
				host = fmt.Sprintf("%s:%d", controlPlaneHost, vCluster.Spec.ControlPlaneEndpoint.Port)
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

	kubeSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-kubeconfig", vCluster.Name),
			Namespace: vCluster.Namespace,
			Labels: map[string]string{
				clusterv1beta1.ClusterNameLabel: vCluster.Name,
			},
		},
		Type: clusterv1beta1.ClusterSecretType,
	}
	_, err = controllerutil.CreateOrPatch(ctx, r.Client, kubeSecret, func() error {
		if kubeSecret.Data == nil {
			kubeSecret.Data = make(map[string][]byte)
		}
		kubeSecret.Data[KubeconfigDataName] = outKubeConfig
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("can not create a kubeconfig secret: %w", err)
	}

	conditions.MarkTrue(vCluster, v1alpha1.KubeconfigReadyCondition)
	return restConfig, nil
}

func (r *VClusterReconciler) checkReadyz(vCluster *v1alpha1.VCluster, restConfig *rest.Config) (bool, error) {
	t := time.Now()
	transport, err := rest.TransportFor(restConfig)
	if err != nil {
		return false, err
	}
	client := r.HTTPClientGetter.ClientFor(transport, 10*time.Second)
	resp, err := client.Get(fmt.Sprintf("https://%s:%d/readyz", vCluster.Spec.ControlPlaneEndpoint.Host, vCluster.Spec.ControlPlaneEndpoint.Port))
	r.Log.V(1).Info("ready check done", "namespace", vCluster.Namespace, "name", vCluster.Name, "duration", time.Since(t))
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

func DiscoverHostFromService(ctx context.Context, client client.Client, vCluster *v1alpha1.VCluster) (string, error) {
	host := ""
	err := wait.PollUntilContextTimeout(ctx, time.Second*2, time.Second*10, true, func(ctx context.Context) (done bool, err error) {
		service := &corev1.Service{}
		err = client.Get(ctx, types.NamespacedName{Namespace: vCluster.Namespace, Name: vCluster.Name}, service)
		if err != nil {
			if kerrors.IsNotFound(err) {
				return true, nil
			}

			return false, err
		}

		// not a load balancer? Then don't wait
		if service.Spec.Type != corev1.ServiceTypeLoadBalancer {
			return true, nil
		}

		if len(service.Status.LoadBalancer.Ingress) == 0 {
			// Waiting for vcluster LoadBalancer ip
			return false, nil
		}

		if service.Status.LoadBalancer.Ingress[0].Hostname != "" {
			host = service.Status.LoadBalancer.Ingress[0].Hostname
		} else if service.Status.LoadBalancer.Ingress[0].IP != "" {
			host = service.Status.LoadBalancer.Ingress[0].IP
		}

		if host == "" {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return "", fmt.Errorf("can not get vcluster service: %w", err)
	}

	if host == "" {
		host = fmt.Sprintf("%s.%s", vCluster.Name, vCluster.Namespace)
	}
	return host, nil
}

func GetVClusterKubeConfig(ctx context.Context, clusterClient client.Client, vCluster *v1alpha1.VCluster) (*api.Config, error) {
	// NOTE: The prefix must be kept in sync with https://github.com/loft-sh/vcluster/blob/main/pkg/util/kubeconfig/kubeconfig.go#L29
	secretName := "vc-" + vCluster.Name

	secret := &corev1.Secret{}
	err := clusterClient.Get(ctx, types.NamespacedName{Namespace: vCluster.Namespace, Name: secretName}, secret)
	if err != nil {
		return nil, err
	}

	// NOTE: The Data map key must be kept in sync with https://github.com/loft-sh/vcluster/blob/main/pkg/util/kubeconfig/kubeconfig.go#L30
	kcBytes, ok := secret.Data["config"]
	if !ok {
		return nil, fmt.Errorf("couldn't find kube config in vcluster secret")
	}

	kubeConfig, err := clientcmd.Load(kcBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to load vcluster kube config: %w", err)
	}

	return kubeConfig, nil
}

func GetVClusterCredentials(ctx context.Context, clusterClient client.Client, vCluster *v1alpha1.VCluster) (*Credentials, error) {
	kubeConfig, err := GetVClusterKubeConfig(ctx, clusterClient, vCluster)
	if err != nil {
		return nil, err
	}

	for _, authInfo := range kubeConfig.AuthInfos {
		if authInfo.ClientKeyData != nil && authInfo.ClientCertificateData != nil {
			return &Credentials{
				ClientCert: authInfo.ClientCertificateData,
				ClientKey:  authInfo.ClientKeyData,
			}, nil
		}
	}

	return nil, fmt.Errorf("couldn't parse kube config, because it seems the vcluster kube config is invalid and missing client cert & client key")
}

func (r *VClusterReconciler) deleteHelmChart(ctx context.Context, namespace, name string) error {
	release, err := r.HelmSecrets.Get(ctx, name, namespace)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}

		return nil
	}

	if release.Secret.Labels == nil || release.Secret.Labels["owner"] != "helm" {
		return nil
	}

	r.Log.Info("delete vcluster helm release",
		"namespace", namespace,
		"name", name,
	)
	return r.HelmClient.Delete(name, namespace)
}

func patchCluster(ctx context.Context, patchHelper *patch.Helper, vCluster *v1alpha1.VCluster, options ...patch.Option) error {
	// Always update the readyCondition by summarizing the state of other conditions.
	conditions.SetSummary(vCluster,
		conditions.WithConditions(
			v1alpha1.KubeconfigReadyCondition,
			v1alpha1.ControlPlaneInitializedCondition,
		),
	)

	// Patch the object, ignoring conflicts on the conditions owned by this controller.
	// Also, if requested, we are adding additional options like e.g. Patch ObservedGeneration when issuing the
	// patch at the end of the reconcile loop.
	options = append(options,
		patch.WithOwnedConditions{Conditions: []v1alpha1.ConditionType{
			v1alpha1.ReadyCondition,
			v1alpha1.KubeconfigReadyCondition,
			v1alpha1.ControlPlaneInitializedCondition,
			v1alpha1.HelmChartDeployedCondition,
		}},
	)
	return patchHelper.Patch(ctx, vCluster, options...)
}

func RemoveFinalizer(ctx context.Context, client client.Client, obj client.Object, finalizer string) error {
	finalizers := obj.GetFinalizers()
	if len(finalizers) > 0 {
		newFinalizers := []string{}
		for _, f := range finalizers {
			if f == finalizer {
				continue
			}
			newFinalizers = append(newFinalizers, f)
		}

		if len(newFinalizers) != len(finalizers) {
			obj.SetFinalizers(newFinalizers)
			err := client.Update(ctx, obj)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func EnsureFinalizer(ctx context.Context, client client.Client, obj client.Object, finalizer string) error {
	found := false
	for _, f := range obj.GetFinalizers() {
		if f == finalizer {
			found = true
			break
		}
	}
	if !found {
		newFinalizers := []string{}
		newFinalizers = append(newFinalizers, obj.GetFinalizers()...)
		newFinalizers = append(newFinalizers, finalizer)
		obj.SetFinalizers(newFinalizers)
		return client.Update(ctx, obj)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error
	r.clusterKindExists, err = kindExists(mgr.GetConfig(), clusterv1beta1.GroupVersion.WithKind("Cluster"))
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.VCluster{}).
		Complete(r)
}

func kindExists(config *rest.Config, groupVersionKind schema.GroupVersionKind) (bool, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return false, err
	}

	resources, err := discoveryClient.ServerResourcesForGroupVersion(groupVersionKind.GroupVersion().String())
	if err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	for _, r := range resources.APIResources {
		if r.Kind == groupVersionKind.Kind {
			return true, nil
		}
	}

	return false, nil
}
