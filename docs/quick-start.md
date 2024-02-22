# Quick Start
In this guide we will deploy Nginx in a kind cluster and verify connectivity with curl.

## Prerequisites
Ensure that you have the following installed:
- [clusterctl](https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl) v1.2 or greater
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- [kind](https://kind.sigs.k8s.io/)
- [vcluster CLI](https://www.vcluster.com/docs/getting-started/setup)
- [curl](https://curl.se/)

Ensure that `clusterctl` has the `vcluster` provider repository configured.
```shell
clusterctl config repositories | grep vcluster
vcluster       InfrastructureProvider   https://github.com/loft-sh/cluster-api-provider-vcluster/releases/latest/                 infrastructure-components.yaml
```
If the output is empty, please upgrade your `clusterctl` version.

## Instructions
Using `kind` we will create a management cluster for Cluster API to use.
```shell 
kind create cluster

Creating cluster "kind" ...
 ✓ Ensuring node image (kindest/node:v1.23.4) 🖼
 ✓ Preparing nodes 📦  
 ✓ Writing configuration 📜 
 ✓ Starting control-plane 🕹️ 
 ✓ Installing CNI 🔌 
 ✓ Installing StorageClass 💾 
Set kubectl context to "kind-kind"
You can now use your cluster with:

kubectl cluster-info --context kind-kind

Thanks for using kind! 😊
```

Verify that `kubectl` has been correctly configured by running.

```shell
# Checking the current context
kubectl config current-context 
kind-kind

# Getting the namespaces in the new cluster
kubectl get namespace
NAME                 STATUS   AGE
default              Active   110s
kube-node-lease      Active   111s
kube-public          Active   111s
kube-system          Active   111s
local-path-storage   Active   108s
```

Install the `vcluster` provider in the cluster.
```shell
clusterctl init --infrastructure vcluster
```

Next we will create our virtual cluster within our `kind` cluster.
```shell
export CLUSTER_NAME=kind
export CLUSTER_NAMESPACE=vcluster
export KUBERNETES_VERSION=1.23.4 
export HELM_VALUES="service:\n  type: NodePort"

kubectl create namespace ${CLUSTER_NAMESPACE}
clusterctl generate cluster ${CLUSTER_NAME} \
    --infrastructure vcluster \
    --kubernetes-version ${KUBERNETES_VERSION} \
    --target-namespace ${CLUSTER_NAMESPACE} | kubectl apply -f -
```

We can verify the cluster by checking the host clusters namespaces.
```shell
kubectl get namespace
NAME                                   STATUS   AGE
capi-kubeadm-bootstrap-system          Active   2m12s
capi-kubeadm-control-plane-system      Active   2m12s
capi-system                            Active   2m12s
cert-manager                           Active   2m23s
cluster-api-provider-vcluster-system   Active   2m11s
default                                Active   7m4s
kube-node-lease                        Active   7m5s
kube-public                            Active   7m5s
kube-system                            Active   7m5s
local-path-storage                     Active   7m2s
vcluster                               Active   48s
```

`vcluster` is the namespace for our virtual cluster. Now lets attempt to use it.

```shell
vcluster connect kind -n vcluster

info   Starting proxy container...
done √ Switched active kube context to vcluster_kind_vcluster_kind-kind
- Use `vcluster disconnect` to return to your previous kube context
- Use `kubectl get namespaces` to access the vcluster
```

Now let's verify that we're in the virtual cluster context.

```shell
kubectl get namespace

NAME              STATUS   AGE
kube-system       Active   3m18s
default           Active   3m18s
kube-public       Active   3m18s
kube-node-lease   Active   3m18s
```

We are now going to deploy nginx to our virtual cluster. 

```shell
# First we create a namespace for nginx
kubectl create namespace demo-nginx

namespace/demo-nginx created
```

```shell
# Then let's create the deployment for nginx
kubectl create deployment nginx-deployment -n demo-nginx --image=nginx

deployment.apps/nginx-deployment created
```

In order for us to verfiy the deployment of nginx we're going to port-forward into the virtual cluster.

**Keep the following command running for the duration of this tutorial**
```shell
kubectl port-forward -n demo-nginx deployment/nginx-deployment 8080:80

Forwarding from 127.0.0.1:8080 -> 80
Forwarding from [::1]:8080 -> 80
```

Then in a new shell run the following `curl` command.
```shell
curl localhost:8080
<!DOCTYPE html>
<html>
<head>
<title>Welcome to nginx!</title>
<style>
html { color-scheme: light dark; }
body { width: 35em; margin: 0 auto;
font-family: Tahoma, Verdana, Arial, sans-serif; }
</style>
</head>
<body>
<h1>Welcome to nginx!</h1>
<p>If you see this page, the nginx web server is successfully installed and
working. Further configuration is required.</p>

<p>For online documentation and support please refer to
<a href="http://nginx.org/">nginx.org</a>.<br/>
Commercial support is available at
<a href="http://nginx.com/">nginx.com</a>.</p>

<p><em>Thank you for using nginx.</em></p>
</body>
</html>
```

We now have `nginx` running in a virtual cluster and verified it's running.

## Cleanup
Terminate the two commands that we left running in the steps before.
* `kubectl port-forward -n demo-nginx deployment/nginx-deployment 8080:80`
* `vcluster connect kind -n vcluster`

If you want to delete the kind cluster you can do so by running:
```shell 
# Delete the cluster by running 
kind delete clusters kind

# Verify there isn't any cluster left
kind get clusters
```
