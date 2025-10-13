package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/loft-sh/cluster-api-provider-vcluster/pkg/util/conditions"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	clusterapiconditions "sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	infrav1 "github.com/loft-sh/cluster-api-provider-vcluster/api/infrastructure/v1alpha1"
)

// VNodeMachineReconciler reconciles a VNodeMachine object.
type VNodeMachineReconciler struct {
	Client client.Client
	Log    logr.Logger
}

// Reconcile handles KubevirtMachine events.
func (r *VNodeMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := r.Log.WithValues("machine", req.NamespacedName)

	// Fetch the VNodeMachine instance.
	vNodeMachine := &infrav1.VNodeMachine{}
	if err := r.Client.Get(ctx, req.NamespacedName, vNodeMachine); err != nil {
		if kerrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Machine.
	machine, err := util.GetOwnerMachine(ctx, r.Client, vNodeMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if machine == nil {
		log.Info("Waiting for Machine Controller to set OwnerRef on VNodeMachine")
		return ctrl.Result{}, nil
	}

	// Handle deleted machines
	if !vNodeMachine.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, vNodeMachine, log)
	}

	// Fetch the Cluster.
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		log.Info("KubevirtMachine owner Machine is missing cluster label or cluster does not exist")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info(fmt.Sprintf("Please associate this machine with a cluster using the label %s: <name of cluster>", clusterv1.ClusterNameLabel))
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", cluster.Name)

	// Fetch the VNodeCluster.
	vNodeCluster := &infrav1.VNodeCluster{}
	vNodeClusterName := client.ObjectKey{
		Namespace: vNodeMachine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(ctx, vNodeClusterName, vNodeCluster); err != nil {
		log.Info("VNodeCluster is not available yet")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("vnode-cluster", vNodeCluster.Name)

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(vNodeMachine, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always attempt to Patch the KubevirtMachine object and status after each reconciliation.
	defer func() {
		if err := r.PatchVNodeMachine(ctx, patchHelper, vNodeMachine); err != nil {
			log.Error(err, "failed to patch VNodeMachine")
			if rerr == nil {
				rerr = err
			}
		}
	}()

	// Add finalizer first if not exist to avoid the race condition between init and delete
	if !controllerutil.ContainsFinalizer(vNodeMachine, infrav1.MachineFinalizer) {
		controllerutil.AddFinalizer(vNodeMachine, infrav1.MachineFinalizer)
		return ctrl.Result{}, nil
	}

	// Check if the infrastructure is ready, otherwise return and wait for the cluster object to be updated
	if !clusterapiconditions.IsTrue(cluster, clusterv1.InfrastructureReadyCondition) {
		log.Info("Waiting for VNodeCluster Controller to create cluster infrastructure")
		conditions.MarkFalse(vNodeMachine, infrav1.PodProvisionedCondition, "WaitForClusterInfrastructure", infrav1.ConditionSeverityInfo, "")
		return ctrl.Result{}, nil
	}

	// Handle non-deleted machines
	return r.reconcileNormal(ctx, vNodeMachine, machine, cluster, log)
}

func (r *VNodeMachineReconciler) reconcileNormal(ctx context.Context, vNodeMachine *infrav1.VNodeMachine, machine *clusterv1.Machine, cluster *clusterv1.Cluster, log logr.Logger) (res ctrl.Result, retErr error) {
	// Make sure bootstrap data is available and populated.
	if machine.Spec.Bootstrap.DataSecretName == nil {
		if !util.IsControlPlaneMachine(machine) && ptr.Equal(cluster.Status.Initialization.ControlPlaneInitialized, ptr.To(false)) {
			log.Info("Waiting for the control plane to be initialized...")
			conditions.MarkFalse(vNodeMachine, infrav1.PodProvisionedCondition, clusterv1.WaitingForControlPlaneInitializedReason, infrav1.ConditionSeverityInfo, "")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		log.Info("Waiting for Machine.Spec.Bootstrap.DataSecretName...")
		conditions.MarkFalse(vNodeMachine, infrav1.PodProvisionedCondition, clusterv1.WaitingForBootstrapDataReason, infrav1.ConditionSeverityInfo, "")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// get data secret
	dataSecret := &corev1.Secret{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: *machine.Spec.Bootstrap.DataSecretName, Namespace: machine.Namespace}, dataSecret)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get data secret: %w", err)
	}

	// now make sure there is a secret with the userdata for the pod
	dataSecretPod := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vNodeMachine.Name + "-userdata",
			Namespace: machine.Namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, dataSecretPod, func() error {
		if dataSecretPod.Data == nil {
			dataSecretPod.Data = make(map[string][]byte)
		}
		dataSecretPod.Data["user-data"] = dataSecret.Data["value"]
		dataSecretPod.Data["meta-data"] = []byte("{}")
		return ctrl.SetControllerReference(vNodeMachine, dataSecretPod, r.Client.Scheme())
	})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create or update data secret pod: %w", err)
	}

	// If there is not a namespace explicitly set on the vm template, then
	// use the infra namespace as a default. For internal clusters, the infraNamespace
	// will be the same as the KubeVirtCluster object, for external clusters the
	// infraNamespace will attempt to be detected from the infraClusterSecretRef's
	// kubeconfig
	podNamespace := vNodeMachine.Spec.PodTemplate.Namespace
	if podNamespace == "" {
		podNamespace = vNodeMachine.Namespace
	}

	// check if the pod already exists
	pod := &corev1.Pod{}
	err = r.Client.Get(ctx, client.ObjectKey{Name: vNodeMachine.Name, Namespace: podNamespace}, pod)
	if err != nil && !kerrors.IsNotFound(err) {
		return ctrl.Result{}, errors.Wrap(err, "failed to get Pod")
	} else if kerrors.IsNotFound(err) {
		// convert the raw extension to pod spec
		rawSpec, err := json.Marshal(vNodeMachine.Spec.PodTemplate.Spec)
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to marshal pod spec")
		}

		// unmarshal the raw spec to pod spec
		podSpec := &corev1.PodSpec{}
		err = json.Unmarshal(rawSpec, podSpec)
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to unmarshal pod spec")
		}

		// add the userdata secret to the pod spec
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: "user-data",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: dataSecretPod.Name,
					Items: []corev1.KeyToPath{
						{
							Key:  "user-data",
							Path: "user-data",
						},
						{
							Key:  "meta-data",
							Path: "meta-data",
						},
					},
				},
			},
		})
		podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      "user-data",
			ReadOnly:  true,
			MountPath: "/var/lib/cloud/seed/nocloud",
		})

		// create the pod
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vNodeMachine.Name,
				Namespace: podNamespace,
			},
			Spec: *podSpec,
		}
		pod.Labels = vNodeMachine.Spec.PodTemplate.Labels
		pod.Annotations = vNodeMachine.Spec.PodTemplate.Annotations

		// create the pod
		err = r.Client.Create(ctx, pod)
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to create Pod")
		}
	}

	// Checks to see if a pod is ready or not
	if pod.Status.Phase == corev1.PodRunning {
		// Mark PodProvisionedCondition to indicate that the pod has successfully started
		conditions.MarkTrue(vNodeMachine, infrav1.PodProvisionedCondition)
	} else {
		reason, message := pod.Status.Reason, pod.Status.Message
		conditions.MarkFalse(vNodeMachine, infrav1.PodProvisionedCondition, reason, infrav1.ConditionSeverityInfo, "%s", message)

		// Waiting for pod to boot
		vNodeMachine.Status.Ready = false
		log.Info("vNodeMachine is not fully provisioned and running...")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	ipAddress := pod.Status.PodIP
	if ipAddress == "" {
		log.Info(fmt.Sprintf("vNodeMachine %s: Got empty ipAddress, requeue", vNodeMachine.Name))
		// Only set readiness to false if we have never detected an internal IP for this machine.
		//
		// The internal ipAddress is sometimes detected via the qemu guest agent,
		// which will report an empty addr at some points when the guest is rebooting
		// or updating.
		//
		// This check prevents us from marking the infrastructure as not ready
		// when the internal guest might be rebooting or updating.
		if !machineHasKnownInternalIP(vNodeMachine) {
			vNodeMachine.Status.Ready = false
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	vNodeMachine.Status.Addresses = []clusterv1.MachineAddress{
		{
			Type:    clusterv1.MachineInternalIP,
			Address: ipAddress,
		},
		{
			Type:    clusterv1.MachineExternalIP,
			Address: ipAddress,
		},
	}

	if vNodeMachine.Spec.ProviderID == nil || *vNodeMachine.Spec.ProviderID == "" {
		// Set ProviderID so the Cluster API Machine Controller can pull it.
		vNodeMachine.Spec.ProviderID = ptr.To("vcluster://" + pod.Name)
	}

	// Ready should reflect if the VMI is ready or not
	if pod.Status.Phase == corev1.PodRunning {
		vNodeMachine.Status.Ready = true
	} else {
		vNodeMachine.Status.Ready = false
	}

	return ctrl.Result{}, nil
}

func machineHasKnownInternalIP(vNodeMachine *infrav1.VNodeMachine) bool {
	for _, addr := range vNodeMachine.Status.Addresses {
		if addr.Type == clusterv1.MachineInternalIP && addr.Address != "" {
			return true
		}
	}
	return false
}

func (r *VNodeMachineReconciler) reconcileDelete(ctx context.Context, vNodeMachine *infrav1.VNodeMachine, log logr.Logger) (ctrl.Result, error) {
	patchHelper, err := patch.NewHelper(vNodeMachine, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// If there is not a namespace explicitly set on the pod template, then
	// use the infra namespace as a default. For internal clusters, the infraNamespace
	// will be the same as the VNodeMachine object, for external clusters the
	// podNamespace will attempt to be detected from the podTemplate's namespace
	podNamespace := vNodeMachine.Spec.PodTemplate.Namespace
	if podNamespace == "" {
		podNamespace = vNodeMachine.Namespace
	}

	vNodePod := &corev1.Pod{}
	err = r.Client.Get(ctx, client.ObjectKey{Name: vNodeMachine.Name, Namespace: podNamespace}, vNodePod)
	if err != nil && !kerrors.IsNotFound(err) {
		if kerrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, errors.Wrap(err, "failed to get Pod")
	} else if err == nil {
		if !vNodePod.DeletionTimestamp.IsZero() {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		log.Info("Deleting Pod...")
		err = r.Client.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: vNodeMachine.Name, Namespace: podNamespace}})
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to delete Pod")
		}

		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Machine is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(vNodeMachine, infrav1.MachineFinalizer)

	// Set the PodProvisionedCondition reporting delete is started, and attempt to issue a patch in
	// order to make this visible to the users.
	conditions.MarkFalse(vNodeMachine, infrav1.PodProvisionedCondition, clusterv1.DeletingReason, infrav1.ConditionSeverityInfo, "")
	if err := r.PatchVNodeMachine(ctx, patchHelper, vNodeMachine); err != nil {
		if err = utilerrors.FilterOut(err, kerrors.IsNotFound); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to patch VNodeMachine")
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager will add watches for this controller.
func (r *VNodeMachineReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	clusterToVNodeMachines, err := util.ClusterToTypedObjectsMapper(mgr.GetClient(), &infrav1.VNodeMachineList{}, mgr.GetScheme())
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.VNodeMachine{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPaused(r.Client.Scheme(), ctrl.LoggerFrom(ctx))).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(util.MachineToInfrastructureMapFunc(infrav1.GroupVersion.WithKind("KubevirtMachine"))),
		).
		Watches(
			&infrav1.VNodeCluster{},
			handler.EnqueueRequestsFromMapFunc(r.VNodeClusterToVNodeMachines),
		).
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(clusterToVNodeMachines),
			builder.WithPredicates(predicates.ClusterPausedTransitionsOrInfrastructureProvisioned(r.Client.Scheme(), ctrl.LoggerFrom(ctx))),
		).
		Complete(r)
}

// PatchVNodeCluster patches the VNodeCluster object and status.
func (r *VNodeMachineReconciler) PatchVNodeMachine(ctx context.Context, patchHelper *patch.Helper, vNodeMachine *infrav1.VNodeMachine) error {
	// Always update the readyCondition by summarizing the state of other conditions.
	// A step counter is added to represent progress during the provisioning process (instead we are hiding it during the deletion process).
	conditions.SetSummary(vNodeMachine, conditions.WithConditions(infrav1.ReadyCondition, infrav1.PodProvisionedCondition))

	// Patch the object, ignoring conflicts on the conditions owned by this controller.
	return patchHelper.Patch(
		ctx,
		vNodeMachine,
		patch.WithOwnedConditions{Conditions: []string{
			string(infrav1.ReadyCondition),
			string(infrav1.PodProvisionedCondition),
		}},
	)
}

// VNodeClusterToVNodeMachines is a handler.ToRequestsFunc to be used to enqueue
// requests for reconciliation of VNodeMachines.
func (r *VNodeMachineReconciler) VNodeClusterToVNodeMachines(ctx context.Context, o client.Object) []ctrl.Request {
	var result []ctrl.Request
	c, ok := o.(*infrav1.VNodeCluster)
	if !ok {
		panic(fmt.Sprintf("Expected a KubevirtCluster but got a %T", o))
	}

	cluster, err := util.GetOwnerCluster(ctx, r.Client, c.ObjectMeta)
	switch {
	case kerrors.IsNotFound(err) || cluster == nil:
		return result
	case err != nil:
		return result
	}

	labels := map[string]string{clusterv1.ClusterNameLabel: cluster.Name}
	machineList := &clusterv1.MachineList{}
	if err := r.Client.List(ctx, machineList, client.InNamespace(c.Namespace), client.MatchingLabels(labels)); err != nil {
		return nil
	}
	for _, m := range machineList.Items {
		if m.Spec.InfrastructureRef.Name == "" {
			continue
		}
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}

	return result
}
