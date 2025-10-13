package controllers

import (
	"context"
	"fmt"

	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/helm"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/kubeconfighelper"
	"github.com/loft-sh/log/logr"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	controlplanev1alpha1 "github.com/loft-sh/cluster-api-provider-vcluster/api/controlplane/v1alpha1"
	infrastructurev1alpha1 "github.com/loft-sh/cluster-api-provider-vcluster/api/infrastructure/v1alpha1"
)

func RegisterControllers(ctx context.Context, mgr manager.Manager) error {
	rawConfig, err := kubeconfighelper.ConvertRestConfigToRawConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("unable to get config: %w", err)
	}

	log, err := logr.NewLoggerWithOptions(
		logr.WithOptionsFromEnv(),
		logr.WithComponentName("vcluster-controller"),
	)
	if err != nil {
		return fmt.Errorf("unable to setup logger: %w", err)
	}

	// infrastructure vCluster
	if err = (&GenericReconciler{
		ControllerName:     "infrastructure-vcluster",
		Client:             mgr.GetClient(),
		HelmClient:         helm.NewClient(rawConfig),
		HelmSecrets:        helm.NewSecrets(mgr.GetClient()),
		Log:                log,
		Scheme:             mgr.GetScheme(),
		ClientConfigGetter: NewClientConfigGetter(),
		HTTPClientGetter:   NewHTTPClientGetter(),
		New: func() GenericVCluster {
			return &infrastructurev1alpha1.VCluster{}
		},
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller: %w", err)
	}

	// controlplane vCluster
	if err = (&GenericReconciler{
		ControllerName:     "controlplane-vcluster",
		Client:             mgr.GetClient(),
		HelmClient:         helm.NewClient(rawConfig),
		HelmSecrets:        helm.NewSecrets(mgr.GetClient()),
		Log:                log,
		Scheme:             mgr.GetScheme(),
		ClientConfigGetter: NewClientConfigGetter(),
		HTTPClientGetter:   NewHTTPClientGetter(),
		New: func() GenericVCluster {
			return &controlplanev1alpha1.VCluster{}
		},
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller: %w", err)
	}

	// vNodeMachine
	if err = (&VNodeMachineReconciler{
		Client: mgr.GetClient(),
		Log:    log,
	}).SetupWithManager(ctx, mgr, controller.Options{}); err != nil {
		return fmt.Errorf("unable to create controller: %w", err)
	}

	// vNodeCluster
	if err = (&VNodeClusterReconciler{
		Client: mgr.GetClient(),
		Log:    log,
	}).SetupWithManager(ctx, mgr, controller.Options{}); err != nil {
		return fmt.Errorf("unable to create controller: %w", err)
	}

	return nil
}
