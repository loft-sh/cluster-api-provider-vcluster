```
# Install CAPI
clusterctl init

VCLUSTER_VERSION=v0.30.0-alpha.2 \
clusterctl generate cluster test --kubernetes-version v1.32.7 -n test --from file://$(pwd)/template.yaml | kubectl apply -f -
```