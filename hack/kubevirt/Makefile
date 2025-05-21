.PHONY: create-vcluster
create-vcluster:
	kubectl create namespace test || true
	
	VCLUSTER_VERSION=v0.26.0-alpha.5 \
	clusterctl generate cluster test --kubernetes-version v1.31.2 -n test --from file://$$(pwd)/template.yaml | kubectl apply -f -

.PHONY: delete-vcluster
delete-vcluster:
	VCLUSTER_VERSION=v0.26.0-alpha.5 \
	clusterctl generate cluster test --kubernetes-version v1.31.2 -n test --from file://$$(pwd)/template.yaml | kubectl delete --ignore-not-found -f -

.PHONY: install-capa
install-capa:
	# 1) Install clusterctl if it's not already on PATH
	if ! command -v clusterctl >/dev/null 2>&1; then \
	  echo "Installing clusterctl…"; \
	  curl -L -o clusterctl \
	    "https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.10.1/clusterctl-$(shell go env GOOS)-$(shell go env GOARCH)"; \
	  sudo install -m 0755 clusterctl /usr/local/bin; \
	  rm -f clusterctl; \
	fi;
	
	# 2) Install KubeVirt
	KV_VER=$$(curl "https://api.github.com/repos/kubevirt/kubevirt/releases/latest" | jq -r ".tag_name") && \
	kubectl apply -f "https://github.com/kubevirt/kubevirt/releases/download/$${KV_VER}/kubevirt-operator.yaml" && \
	kubectl apply -f "https://github.com/kubevirt/kubevirt/releases/download/$${KV_VER}/kubevirt-cr.yaml" && \
	kubectl wait -n kubevirt kv kubevirt --for=condition=Available --timeout=10m
	
	# 3) Install KubeVirt CDI
	CDI_TAG=$$(curl -s -w %{redirect_url} https://github.com/kubevirt/containerized-data-importer/releases/latest) && \
	CDI_VERSION=$$(echo $${CDI_TAG##*/}) && \
	kubectl create -f https://github.com/kubevirt/containerized-data-importer/releases/download/$$CDI_VERSION/cdi-operator.yaml && \	
	kubectl create -f https://github.com/kubevirt/containerized-data-importer/releases/download/$$CDI_VERSION/cdi-cr.yaml
	
	# 3) Init capi
	clusterctl init --infrastructure kubevirt
