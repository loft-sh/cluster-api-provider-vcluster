package controllers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	infrastructurev1alpha1 "github.com/loft-sh/cluster-api-provider-vcluster/api/infrastructure/v1alpha1"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/helm"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/conditions"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/patch"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// A finalizer that is added to the VCluster CR to ensure that helm delete is executed.
	CleanupFinalizer = "vcluster.loft.sh/cleanup"

	// DefaultControlPlanePort is the default port for the control plane.
	DefaultControlPlanePort = 443

	// KubeconfigDataName is the key used to store a Kubeconfig in the secret's data field.
	KubeconfigDataName = "value"

	// TLSKeyDataName is the key used to store a TLS private key in the secret's data field.
	TLSKeyDataName = "tls.key"

	// TLSCrtDataName is the key used to store a TLS certificate in the secret's data field.
	TLSCrtDataName = "tls.crt"
)

type Credentials struct {
	ClientCert []byte
	ClientKey  []byte
}

type ClientConfigGetter interface {
	NewForConfig(restConfig *rest.Config) (kubernetes.Interface, error)
}

type clientConfigGetter struct{}

func (c *clientConfigGetter) NewForConfig(restConfig *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(restConfig)
}

func NewClientConfigGetter() ClientConfigGetter {
	return &clientConfigGetter{}
}

type HTTPClientGetter interface {
	ClientFor(r http.RoundTripper, timeout time.Duration) *http.Client
}

type httpClientGetter struct{}

func (h *httpClientGetter) ClientFor(r http.RoundTripper, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: r,
	}
}

func NewHTTPClientGetter() HTTPClientGetter {
	return &httpClientGetter{}
}

func deleteHelmChart(ctx context.Context, helmClient helm.Client, helmSecrets *helm.Secrets, namespace, name string, log logr.Logger) error {
	release, err := helmSecrets.Get(ctx, name, namespace)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}

		return nil
	}

	if release.Secret.Labels == nil || release.Secret.Labels["owner"] != "helm" {
		return nil
	}

	log.Info("delete vcluster helm release",
		"namespace", namespace,
		"name", name,
	)
	return helmClient.Delete(name, namespace)
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

func patchCluster(ctx context.Context, patchHelper *patch.Helper, vCluster conditions.Setter, options ...patch.Option) error {
	// Always update the readyCondition by summarizing the state of other conditions.
	conditions.SetSummary(vCluster,
		conditions.WithConditions(
			infrastructurev1alpha1.KubeconfigReadyCondition,
			infrastructurev1alpha1.ControlPlaneInitializedCondition,
		),
	)

	// Patch the object, ignoring conflicts on the conditions owned by this controller.
	// Also, if requested, we are adding additional options like e.g. Patch ObservedGeneration when issuing the
	// patch at the end of the reconcile loop.
	options = append(options,
		patch.WithOwnedConditions{Conditions: []infrastructurev1alpha1.ConditionType{
			infrastructurev1alpha1.ReadyCondition,
			infrastructurev1alpha1.KubeconfigReadyCondition,
			infrastructurev1alpha1.ControlPlaneInitializedCondition,
			infrastructurev1alpha1.HelmChartDeployedCondition,
			infrastructurev1alpha1.InfrastructureClusterSyncedCondition,
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

func DiscoverHostFromService(ctx context.Context, client client.Client, vCluster GenericVCluster) (string, error) {
	host := ""
	service := &corev1.Service{}
	err := wait.PollUntilContextTimeout(ctx, time.Second*2, time.Second*30, true, func(ctx context.Context) (done bool, err error) {
		err = client.Get(ctx, types.NamespacedName{Namespace: vCluster.GetNamespace(), Name: vCluster.GetName()}, service)
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

		for _, ingress := range service.Status.LoadBalancer.Ingress {
			// prefer an IP address over a hostname
			if ingress.IP != "" {
				host = ingress.IP
				break
			} else if ingress.Hostname != "" {
				host = ingress.Hostname
				break
			}
		}

		if host == "" {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return "", fmt.Errorf("can not get vcluster service: %w", err)
	}

	if service == nil {
		return "", fmt.Errorf("vcluster service not found")
	}

	if host == "" {
		host = service.Spec.ClusterIP
	}
	return host, nil
}

func GetVClusterKubeConfig(ctx context.Context, clusterClient client.Client, vCluster GenericVCluster) (*api.Config, error) {
	// NOTE: The prefix must be kept in sync with https://github.com/loft-sh/vcluster/blob/main/pkg/util/kubeconfig/kubeconfig.go#L29
	secretName := "vc-" + vCluster.GetName()

	secret := &corev1.Secret{}
	err := clusterClient.Get(ctx, types.NamespacedName{Namespace: vCluster.GetNamespace(), Name: secretName}, secret)
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

func GetVClusterCredentials(ctx context.Context, clusterClient client.Client, vCluster GenericVCluster) (*Credentials, error) {
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
