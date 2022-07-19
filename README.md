# About

This is a [Cluster API](https://cluster-api.sigs.k8s.io/introduction.html) provider for the [vcluster project](https://www.vcluster.com/) - create fully functional virtual Kubernetes clusters.

# Quick Start - Deploying Nginx in a Kind cluster
 Can be found [here](./docs/quick-start.md)

# Installation instructions

Prerequisites:
- [clusterctl](https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- A Kubernetes cluster where you will have cluster-admin permissions
- Optional, depending on how you expose the vcluster instance - [vcluster CLI](https://www.vcluster.com/docs/getting-started/setup)

As a first step, we need to add the cluster-api-provider-vcluster configuration into your local clusterctl configuration file `~/.cluster-api/clusterctl.yaml` as the following:

```yaml
providers:
  - name: vcluster
    url: https://github.com/loft-sh/cluster-api-provider-vcluster/releases/latest/infrastructure-components.yaml
    type: InfrastructureProvider                                                           
```

You should be able to see the vcluster provider when running `clusterctl config repositories`:
```shell
clusterctl config repositories | grep vcluster
vcluster   InfrastructureProvider   https://github.com/loft-sh/cluster-api-provider-vcluster/releases/latest/                    infrastructure-components.yaml
```

Install the vcluster provider

```shell
clusterctl init --infrastructure vcluster
```

Next you will generate a manifest file for a vcluster instance and create it in the management cluster.
Cluster instance is configured using clusterctl parameters and environment variables - CHART_NAME, CHART_REPO, CHART_VERSION, VCLUSTER_HOST and VCLUSTER_PORT.
In the example commands below, the HELM_VALUES variable will be populated with the contents of the `values.yaml` file.
```shell
export CLUSTER_NAME=vcluster
export CLUSTER_NAMESPACE=vcluster
export KUBERNETES_VERSION=1.23.0
export HELM_VALUES=""
# Uncomment if you want to use vcluster values
# export HELM_VALUES=$(cat devvalues.yaml | sed -z 's/\n/\\n/g')
kubectl create namespace ${CLUSTER_NAMESPACE}
clusterctl generate cluster ${CLUSTER_NAME} \
    --infrastructure vcluster \
    --kubernetes-version ${KUBERNETES_VERSION} \
    --target-namespace ${CLUSTER_NAMESPACE} | kubectl apply -f -
```

Now we just need to wait until vcluster custom resource reports ready status:
```shell
kubectl wait --for=condition=ready vcluster -n $CLUSTER_NAMESPACE $CLUSTER_NAME --timeout=300s
```
At this point the cluster is ready to be used. Please refer to the next chapter to get the credentials.

**Note**: at the moment, the provider is able to host vclusters only in the cluster where the vcluter provider is running([management cluster](https://cluster-api.sigs.k8s.io/user/concepts.html#management-cluster)). Support for the remote host clusters is on our roadmap - [loft-sh/cluster-api-provider-vcluster#6](https://github.com/loft-sh/cluster-api-provider-vcluster/issues/6).

# How to connect to your vcluster
There are multiple methods for exposing your vcluster instance, and they are described in the [vcluster docs](https://www.vcluster.com/docs/operator/external-access). If you follow the docs exactly, you will need to use the vcluster CLI to retrieve kubeconfig. When using this CAPI provider you have an alternative - `clusterctl get kubeconfig ${CLUSTER_NAME} --namespace ${CLUSTER_NAMESPACE} > ./kubeconfig.yaml`, more details about this are in the [CAPI docs](https://cluster-api.sigs.k8s.io/clusterctl/commands/get-kubeconfig.html). Virtual cluster kube config will be written to: ./kubeconfig.yaml. You can access the cluster via `kubectl --kubeconfig ./kubeconfig.yaml get namespaces`.

However, if you are not exposing the vcluster instance with an external hostname, but you want to connect to it from outside the cluster, you will need to use the [vcluster CLI](https://www.vcluster.com/docs/getting-started/setup):
```shell
vcluster connect ${CLUSTER_NAME} -n ${CLUSTER_NAMESPACE}
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
Cluster instance is configured from a template file using environment variables - CLUSTER_NAME, KUBERNETES_VERSION, CHART_NAME, CHART_REPO, CHART_VERSION, VCLUSTER_HOST and VCLUSTER_PORT. Only the CLUSTER_NAME variable is mandatory.
In the example commands below, the HELM_VALUES variable will be populated with the contents of the `devvalues.yaml` file, don't forget to re-run the `export HELM_VALUES...` command when the `devvalues.yaml` changes.
```shell
export CLUSTER_NAME=test
export CLUSTER_NAMESPACE=test
export KUBERNETES_VERSION=1.24.0
export HELM_VALUES=$(cat devvalues.yaml | sed -z 's/\n/\\n/g')
kubectl create namespace ${CLUSTER_NAMESPACE}
cat templates/cluster-template.yaml | ./bin/envsubst | kubectl apply -n ${CLUSTER_NAMESPACE} -f -
```

Now we just need to wait until VCluster custom resource reports ready status:
```shell
kubectl wait --for=condition=ready vcluster -n $CLUSTER_NAMESPACE $CLUSTER_NAME --timeout=300s
```
At this point the cluster is ready to be used. Please refer to "How to connect to your vcluster" chapter above to get the credentials.
