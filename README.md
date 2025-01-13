# About

This is a [Cluster API](https://cluster-api.sigs.k8s.io/introduction.html) provider for the [vcluster project](https://www.vcluster.com/) - create fully functional virtual Kubernetes clusters.

# Quick Start - Deploying Nginx in a Kind cluster
 Can be found [here](./docs/quick-start.md)

# Installation instructions

Prerequisites:
- [clusterctl](https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl) (v1.9.2+)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- A Kubernetes cluster where you will have cluster-admin permissions
- Optional, depending on how you expose the vcluster instance - [vcluster CLI](https://www.vcluster.com/docs/getting-started/setup)

Install the vcluster provider

```shell
clusterctl init --infrastructure vcluster
```

Next you will generate a manifest file for a vcluster instance and create it in the management cluster.
Cluster instance is configured using clusterctl parameters and environment variables - CHART_NAME, CHART_REPO, CHART_VERSION, VCLUSTER_HOST and VCLUSTER_PORT.
In the example commands below, the VCLUSTER_YAML variable will be populated with the contents of the `values.yaml` file.
```shell
export CLUSTER_NAME=vcluster
export CLUSTER_NAMESPACE=vcluster
export VCLUSTER_YAML=""
# Uncomment if you want to use vcluster values
# export VCLUSTER_YAML=$(cat devvalues.yaml | awk '{printf "%s\\n", $0}')
kubectl create namespace ${CLUSTER_NAMESPACE}
clusterctl generate cluster ${CLUSTER_NAME} \
    --infrastructure vcluster \
    --target-namespace ${CLUSTER_NAMESPACE} | kubectl apply -f -
```

Now we just need to wait until vcluster custom resource reports ready status:
```shell
kubectl wait --for=condition=ready vcluster -n $CLUSTER_NAMESPACE $CLUSTER_NAME --timeout=300s
```
At this point the cluster is ready to be used. Please refer to the next chapter to get the credentials.

**Note**: at the moment, the provider is able to host vclusters only in the cluster where the vcluster provider is running([management cluster](https://cluster-api.sigs.k8s.io/user/concepts.html#management-cluster)). Support for the remote host clusters is on our roadmap - [loft-sh/cluster-api-provider-vcluster#6](https://github.com/loft-sh/cluster-api-provider-vcluster/issues/6).

# How to connect to your vcluster
There are multiple methods for exposing your vcluster instance, and they are described in the [vcluster docs](https://www.vcluster.com/docs/operator/external-access). If you follow the docs exactly, you will need to use the vcluster CLI to retrieve kubeconfig. When using this CAPI provider you have an alternative - `clusterctl get kubeconfig ${CLUSTER_NAME} --namespace ${CLUSTER_NAMESPACE} > ./kubeconfig.yaml`, more details about this are in the [CAPI docs](https://cluster-api.sigs.k8s.io/clusterctl/commands/get-kubeconfig.html). You can access the cluster via `kubectl --kubeconfig ./kubeconfig.yaml get namespaces`.

However, if you are not exposing the vcluster instance with an external hostname, but you want to connect to it from outside the cluster, you will need to use the [vcluster CLI](https://www.vcluster.com/docs/getting-started/setup):
```shell
vcluster connect ${CLUSTER_NAME} -n ${CLUSTER_NAMESPACE}
```

# vcluster custom resource example
With the `clusterctl generate cluster` command we are producing a manifest with two Kubernetes custom resources - Cluster (cluster.x-k8s.io/v1beta1) and VCluster (infrastructure.cluster.x-k8s.io/v1alpha1).  
Below you may find an example of these two CRs with the comments explaining important fields.

``` yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: ${CLUSTER_NAME}
spec:
  # Two *Ref fields below must reference VCluster CR by name
  # in order to conform to the CAPI contract   
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
    kind: VCluster
    # name field must match .metadata.name of the VCluster CR
    name: ${CLUSTER_NAME}
  controlPlaneRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
    kind: VCluster
    # name field must match .metadata.name of the VCluster CR
    name: ${CLUSTER_NAME}
---
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: VCluster
metadata:
  name: ${CLUSTER_NAME}
spec:
  # We are using vcluster Helm charts for the installation and upgrade.
  # The helmRelease sub-fields allow you to set Helm values and chart repo, name and version.
  # Sources of the charts can be found here - https://github.com/loft-sh/vcluster/tree/main/charts 
  #
  # This field, and all it's sub-fields, are optional.
  helmRelease:

    # The values field must contain a string with contents that would make a valid YAML file.
    # Please refer to vcluster documentation for the extensive explanation of the features,
    # and the appropriate Helm values that need to be set for your use case - https://www.vcluster.com/docs
    values: |-
      # controlPlane:
      #   distro:
      #     k8s:
      #       enabled: true
      #   backingStore:
      #     etcd:
      #       deploy:
      #         enabled: true

    chart: 
      # By default, the "https://charts.loft.sh" repo is used
      repo: ${CHART_REPO:=https://charts.loft.sh}
      # By default, the "vcluster" chart is used. This coresponds to the "k3s" distro of the
      # vcluster, and the "/charts/k3s" folder in the vcluster GitHub repo.
      # Other available options currently are: "vcluster-k8s", "vcluster-k0s" and "vcluster-eks".
      name: ${CHART_NAME:=vcluster}
      # By default, a particular vcluster version is used in a given CAPVC release. You may find
      # it out from the source code, e.g.: 
      # https://github.com/loft-sh/cluster-api-provider-vcluster/blob/v0.1.3/pkg/constants/constants.go#L7
      #
      # Please refer to the vcluster Releases page for the list of the available versions:
      # https://github.com/loft-sh/vcluster/releases
      version: ${CHART_VERSION:=0.22.1}

  # controlPlaneEndpoint represents the endpoint used to communicate with the control plane.
  # You may leave this field empty, and then CAPVC will try to fill in this information based
  # on the network resources created by the chart (Service, Ingress).
  # The vcluster chart provides options to create a service of LoadBalancer type by setting
  # Helm value - `.service.type: "LoadBalancer"`, or creating an Ingress by setting Helm value
  # `.ingress.enabled: true` and `.ingress.host`. You can explore all options in the charts
  # folder of our vcluster repo - https://github.com/loft-sh/vcluster/tree/main/charts
  #
  # We also recommend reading official vcluster documentation page on this topic:
  # https://www.vcluster.com/docs/operator/external-access
  # this page outlines additional Helm values that you will need to set in certain cases, e.g.
  # `.syncer.extraArgs: ["--tls-san=myvcluster.mydns.abc"]`
  #
  # This field, and all it's sub-fields, are optional.
  controlPlaneEndpoint:
    host: "myvcluster.mydns.abc"
    port: "443"
```

# Development instructions

Prerequisites:
- [Devspace](https://devspace.sh/cli/docs/getting-started/installation)
- [clusterctl](https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- [envsubst](https://github.com/drone/envsubst) - which you can easily install into a local bin directory - `GOBIN="$(pwd)/bin" go install -tags tools github.com/drone/envsubst/v2/cmd/envsubst@v2.0.0-20210730161058-179042472c46`
- A Kubernetes cluster where you will have cluster-admin permissions

First, we install core components of the Cluster API:
```shell
clusterctl init
```

Next we will start a development container for the vcluster provider.
Devspace will continuosly sync local source code into the container, and you will be able to easily and quickly restart the provider with the newest code via the shell that is opened by the following command:

```shell
devspace dev
```

Once the shell is opened, you should see some basic instructions printed.
You can then run the provider with the following command:
```shell
go run -mod vendor main.go
```

Next, in a separate terminal you will generate a manifest file for a vcluster instance.
Cluster instance is configured from a template file using environment variables - CLUSTER_NAME, CHART_NAME, CHART_REPO, CHART_VERSION, VCLUSTER_HOST and VCLUSTER_PORT. Only the CHART_VERSION and CLUSTER_NAME variables are mandatory.
In the example commands below, the VCLUSTER_YAML variable will be populated with the contents of the `devvalues.yaml` file, don't forget to re-run the `export VCLUSTER_YAML...` command when the `devvalues.yaml` changes.
```shell
export CLUSTER_NAME=test
export CLUSTER_NAMESPACE=test
export CHART_VERSION=0.22.1
export CHART_NAME=vcluster
export VCLUSTER_YAML=$(cat devvalues.yaml | awk '{printf "%s\\n", $0}')
kubectl create namespace ${CLUSTER_NAMESPACE}
cat templates/cluster-template.yaml | ./bin/envsubst | kubectl apply -n ${CLUSTER_NAMESPACE} -f -
```

Now we just need to wait until VCluster custom resource reports ready status:
```shell
kubectl wait --for=condition=ready vcluster -n $CLUSTER_NAMESPACE $CLUSTER_NAME --timeout=300s
```
At this point the cluster is ready to be used. Please refer to "How to connect to your vcluster" chapter above to get the credentials.

# Customizing Helm related values

## Specifiying Chart related values

You can specify a custom version of the vCluster Helm chart by setting the CHART_VERSION environment variable. This allows you to pin the vCluster installation to a specific version of the Helm chart.

Example:

```shell
export CHART_VERSION=0.22.1
```

## Specifying custom values for virtual clusters

Depending on your needs, you might want to let CAPVC create a virtual cluster accordingly by e.g. settings those values in a corresponding file that is fed to the `VCLUSTER_YAML` environment variable.

Example:

```shell
cat > /tmp/values.yaml <<EOF
controlPlane:
  distro:
    k8s:
      enabled: true
  backingStore:
    etcd:
      deploy:
        enabled: true
EOF

export VCLUSTER_YAML=$(cat /tmp/values.yaml | awk '{printf "%s\\n", $0}')
```

For all possible values please see the [official docs](https://www.vcluster.com/docs/vcluster/configure/vcluster-yaml/).
