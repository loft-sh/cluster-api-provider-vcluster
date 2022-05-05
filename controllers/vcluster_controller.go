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
	"os"
	"strings"
	"time"

	"github.com/loft-sh/vcluster/pkg/util"
	"github.com/loft-sh/vcluster/pkg/util/kubeconfig"
	"github.com/loft-sh/vcluster/pkg/util/loghelper"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1alpha1 "github.com/loft-sh/cluster-api-provider-vcluster/api/v1alpha1"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/constants"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/helm"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/cidrdiscovery"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/conditions"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/kubeconfighelper"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/patch"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/vclustervalues"
)

// VClusterReconciler reconciles a VCluster object
type VClusterReconciler struct {
	client.Client
	HelmClient        helm.Client
	HelmSecrets       *helm.Secrets
	Log               loghelper.Logger
	Scheme            *runtime.Scheme
	clusterKindExists bool
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
	r.Log.Debugf("Reconcile %s", req.NamespacedName)

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
		r.reconcilePhase(ctx, vCluster)

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
		r.Log.Infof("error during virtual cluster deploy %s/%s: %v", vCluster.Namespace, vCluster.Name, err)
		conditions.MarkFalse(vCluster, v1alpha1.HelmChartDeployedCondition, "HelmDeployFailed", v1alpha1.ConditionSeverityError, "%v", err)
		return ctrl.Result{}, err
	}

	// check if vcluster is reachable and sync the kubeconfig Secret
	t := time.Now()
	err = r.syncVClusterKubeconfig(ctx, vCluster)
	r.Log.Debugf("%s/%s: ready check took: %v", vCluster.Namespace, vCluster.Name, time.Since(t))
	if err != nil {
		r.Log.Debugf("vcluster %s/%s is not ready: %v", vCluster.Namespace, vCluster.Name, err)
		conditions.MarkFalse(vCluster, v1alpha1.KubeconfigReadyCondition, "CheckFailed", v1alpha1.ConditionSeverityWarning, "%v", err)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	return ctrl.Result{}, nil
}

func (r *VClusterReconciler) reconcilePhase(_ context.Context, vCluster *v1alpha1.VCluster) {
	vCluster.Status.Ready = conditions.IsTrue(vCluster, v1alpha1.KubeconfigReadyCondition)

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

func (r *VClusterReconciler) redeployIfNeeded(ctx context.Context, vCluster *v1alpha1.VCluster) error {
	// upgrade chart
	if vCluster.Generation == vCluster.Status.ObservedGeneration && conditions.IsTrue(vCluster, v1alpha1.HelmChartDeployedCondition) {
		return nil
	}

	r.Log.Debugf("upgrade virtual cluster helm chart %s/%s", vCluster.Namespace, vCluster.Name)

	// look up CIDR
	cidr, err := cidrdiscovery.NewCIDRLookup(r.Client).GetServiceCIDR(ctx, vCluster.Namespace)
	if err != nil {
		return fmt.Errorf("get service cidr: %v", err)
	}

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

	// chart version
	var chartVersion string
	if vCluster.Spec.HelmRelease != nil {
		chartVersion = vCluster.Spec.HelmRelease.Chart.Version
	}
	if chartVersion == "" {
		chartVersion = constants.DefaultVClusterVersion
	}
	if len(chartVersion) > 0 && chartVersion[0] == 'v' {
		chartVersion = chartVersion[1:]
	}

	// determine values
	var values string
	if vCluster.Spec.HelmRelease != nil {
		values = vCluster.Spec.HelmRelease.Values
	}

	kVersion := &version.Info{
		Major: "1",
		Minor: "23",
	}
	if vCluster.Spec.KubernetesVersion != nil && *vCluster.Spec.KubernetesVersion != "" {
		v := strings.Split(*vCluster.Spec.KubernetesVersion, ".")
		if len(v) == 2 {
			kVersion.Major = v[0]
			kVersion.Minor = v[1]
		} else {
			return fmt.Errorf("invalid value of the .spec.kubernetesVersion field: %s", *vCluster.Spec.KubernetesVersion)
		}
	}

	//TODO: if .spec.controlPlaneEndpoint.Host is set it would be nice to pass it as --tls-san flag of syncer
	values, err = vclustervalues.NewValuesMerger(
		kVersion,
		cidr,
	).Merge(&v1alpha1.VirtualClusterHelmRelease{
		Chart: v1alpha1.VirtualClusterHelmChart{
			Name:    chartName,
			Repo:    chartRepo,
			Version: chartVersion,
		},
		Values: values,
	}, r.Log)
	if err != nil {
		return fmt.Errorf("merge values: %v", err)
	}

	r.Log.Infof("Deploy virtual cluster %s/%s with values: %s", vCluster.Namespace, vCluster.Name, values)

	chartPath := "./" + chartName + "-" + chartVersion + ".tgz"
	_, err = os.Stat(chartPath)
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

		return fmt.Errorf("error installing / upgrading vcluster: %v", err)
	}

	conditions.MarkTrue(vCluster, v1alpha1.HelmChartDeployedCondition)
	conditions.Delete(vCluster, v1alpha1.KubeconfigReadyCondition)

	return nil
}

func (r *VClusterReconciler) syncVClusterKubeconfig(ctx context.Context, vCluster *v1alpha1.VCluster) error {
	credentials, err := GetVClusterCredentials(ctx, r.Client, vCluster)
	if err != nil {
		return err
	}

	restConfig, err := kubeconfighelper.NewVClusterClientConfig(vCluster.Name, vCluster.Namespace, "", credentials.ClientCert, credentials.ClientKey)
	if err != nil {
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	ctxTimeout, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	// if we haven't checked if the vcluster is initialized, do it now
	if !conditions.IsTrue(vCluster, v1alpha1.ControlPlaneInitializedCondition) {
		_, err = kubeClient.CoreV1().ServiceAccounts("default").Get(ctxTimeout, "default", metav1.GetOptions{})
		if err != nil {
			return err
		}

		conditions.MarkTrue(vCluster, v1alpha1.ControlPlaneInitializedCondition)
	}

	// write kubeconfig to the vcluster.Name+"-kubeconfig" Secret as expected by CAPI convention
	kubeConfig, err := GetVClusterKubeConfig(ctx, r.Client, vCluster)
	if err != nil {
		return fmt.Errorf("can not retrieve kubeconfig: %v", err)
	}
	if len(kubeConfig.Clusters) != 1 {
		return fmt.Errorf("unexpected kube config")
	}

	// If vcluster.spec.controlPlaneEndpoint.Host is not set, try to autodiscover it from
	// the Service that targets vcluster pods, and write it back into the spec.
	controlPlaneHost := vCluster.Spec.ControlPlaneEndpoint.Host
	if controlPlaneHost == "" {
		controlPlaneHost, err = DiscoverHostFromService(ctx, r.Client, vCluster)
		if err != nil {
			return err
		}
		//TODO write back vcluster.spec.controlPlaneEndpoint.Host
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
		return err
	}

	kubeSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-kubeconfig", vCluster.Name), Namespace: vCluster.Namespace}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, kubeSecret, func() error {
		if kubeSecret.Data == nil {
			kubeSecret.Data = make(map[string][]byte)
		}
		kubeSecret.Data[KubeconfigDataName] = outKubeConfig
		return nil
	})
	if err != nil {
		return fmt.Errorf("can not create a kubeconfig secret: %v", err)
	}

	conditions.MarkTrue(vCluster, v1alpha1.KubeconfigReadyCondition)
	return nil
}

func DiscoverHostFromService(ctx context.Context, client client.Client, vCluster *v1alpha1.VCluster) (string, error) {
	host := ""
	err := wait.PollImmediate(time.Second*2, time.Second*10, func() (done bool, err error) {
		service := &corev1.Service{}
		err = client.Get(context.TODO(), types.NamespacedName{Namespace: vCluster.Namespace, Name: vCluster.Name}, service)
		if err != nil {
			// if kerrors.IsNotFound(err) {
			// 	return true, nil
			// }

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
		return "", fmt.Errorf("can not get vcluster service: %v", err)
	}

	return host, nil
}

func GetVClusterKubeConfig(ctx context.Context, clusterClient client.Client, vCluster *v1alpha1.VCluster) (*api.Config, error) {
	secretName := kubeconfig.DefaultSecretPrefix + vCluster.Name

	secret := &corev1.Secret{}
	err := clusterClient.Get(ctx, types.NamespacedName{Namespace: vCluster.Namespace, Name: secretName}, secret)
	if err != nil {
		return nil, err
	}

	kcBytes, ok := secret.Data[kubeconfig.KubeconfigSecretKey]
	if !ok {
		return nil, fmt.Errorf("couldn't find kube config in vcluster secret")
	}

	kubeConfig, err := clientcmd.Load(kcBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to load vcluster kube config: %v", err)
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

	r.Log.Debugf("delete vcluster %s/%s helm release", namespace, name)
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
	r.clusterKindExists, err = util.KindExists(mgr.GetConfig(), clusterv1beta1.GroupVersion.WithKind("Cluster"))
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.VCluster{}).
		Complete(r)
}
