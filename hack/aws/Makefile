# EKS settings
EKS_CLUSTER_NAME ?= cluster-api-test
EKS_REGION       ?= eu-west-1
EKS_NUM_NODES    ?= 1
EKS_VERSION      ?= 1.31
EKS_MACHINE_TYPE ?= t3.xlarge
EKS_AMI_FAMILY   ?= AmazonLinux2023
EKS_SSH_KEY_NAME ?= cluster-api-ssh-key

.PHONY: create-cluster
create-cluster: # Start an EKS cluster
	# Create the EKS cluster
	eksctl create cluster \
      --name $(EKS_CLUSTER_NAME) \
      --version $(EKS_VERSION) \
      --region $(EKS_REGION) \
      --node-type $(EKS_MACHINE_TYPE) \
      --node-ami-family $(EKS_AMI_FAMILY) \
      --nodes $(EKS_NUM_NODES) \
      --managed
	
	# Install local-path-provisioner
	kubectl apply -f https://raw.githubusercontent.com/rancher/local-path-provisioner/v0.0.31/deploy/local-path-storage.yaml
	kubectl patch storageclass local-path \
      --type='merge' \
      -p '{"metadata": {"annotations": {"storageclass.kubernetes.io/is-default-class": "true"}}}'

.PHONY: delete-cluster
delete-cluster:
	eksctl delete cluster \
      --name $(EKS_CLUSTER_NAME) \
      --region $(EKS_REGION)

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
	
	# 2) Bootstrap AWS creds and init CAPI on AWS
	export AWS_REGION=$(EKS_REGION); \
	export AWS_B64ENCODED_CREDENTIALS=$$(AWS_REGION=$(EKS_REGION) clusterawsadm bootstrap credentials encode-as-profile); \
	clusterctl init --infrastructure aws

.PHONY: refresh-credentials
refresh-credentials:
	AWS_REGION=$(EKS_REGION) clusterawsadm bootstrap credentials encode-as-profile | base64 -D > /tmp/credentials.txt; \
	kubectl delete secret -n capa-system capa-manager-bootstrap-credentials --ignore-not-found; \
	kubectl create secret generic capa-manager-bootstrap-credentials -n capa-system --from-file=credentials=/tmp/credentials.txt; \
	rm -f /tmp/credentials.txt; \
	kubectl delete po -l cluster.x-k8s.io/provider=infrastructure-aws -n capa-system --ignore-not-found

.PHONY: create-vcluster
create-vcluster:
	# Create the namespace if it doesn't exist
	kubectl create namespace vcluster-aws-cluster || true
	
	# Generate the cluster and apply it
	VCLUSTER_VERSION=v0.26.0-alpha.4 \
	AWS_NODE_MACHINE_TYPE=$(EKS_MACHINE_TYPE) \
	AWS_REGION=$(EKS_REGION) \
	AWS_SSH_KEY_NAME=$(EKS_SSH_KEY_NAME) \
	WORKER_MACHINE_COUNT=1 \
	clusterctl generate cluster vcluster-aws-cluster --kubernetes-version v1.31.0 -n vcluster-aws-cluster --from file://$$(pwd)/vcluster-aws-template.yaml | kubectl apply -f -

.PHONY: eks-delete-vcluster-capa-cluster
delete-vcluster:
	# Generate the cluster and delete it
	VCLUSTER_VERSION=v0.26.0-alpha.4 \
	AWS_NODE_MACHINE_TYPE=$(EKS_MACHINE_TYPE) \
	AWS_REGION=$(EKS_REGION) \
	AWS_SSH_KEY_NAME=$(EKS_SSH_KEY_NAME) \
	WORKER_MACHINE_COUNT=1 \
	clusterctl generate cluster vcluster-aws-cluster --kubernetes-version v1.31.0 -n vcluster-aws-cluster --from file://$$(pwd)/vcluster-aws-template.yaml | kubectl delete --ignore-not-found -f -

.PHONY: eks-bootstrap-capi-aws-provider
bootstrap-aws-provider:
	# Download clusterawsadm
	@command -v clusterawsadm >/dev/null 2>&1 || { \
		echo "Installing clusterawsadm..."; \
		curl -L -o clusterawsadm \
		  "https://github.com/kubernetes-sigs/cluster-api-provider-aws/releases/download/v2.8.2/clusterawsadm-$(shell go env GOOS)-$(shell go env GOARCH)"; \
		sudo install -c -m 0755 clusterawsadm /usr/local/bin; \
		rm -f clusterawsadm; \
	}

	# Setup the AWS provider
	AWS_REGION=$(EKS_REGION) clusterawsadm bootstrap iam create-cloudformation-stack

	# Create the SSH key pair
	export AWS_REGION=$(EKS_REGION); \
	aws ssm put-parameter --name "/sigs.k8s.io/cluster-api-provider-aws/ssh-key" \
	--type SecureString \
	--value "$$(aws ec2 create-key-pair --key-name $(EKS_SSH_KEY_NAME) --output json | jq .KeyMaterial -r)"
	