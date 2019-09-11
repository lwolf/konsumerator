
VERSION=$(shell git rev-list --count HEAD)-$(shell git rev-parse --short=7 HEAD)
# Image URL to use all building/pushing image targets
IMG ?= quay.io/lwolf/konsumerator:$(VERSION)
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true"

# for some reason travis-ci calls `make` on each step which triggers download
# of all packages. This check disables it.
ifeq ($(CI),true)
all:
	@echo "disabling default target make"
endif


build: manager

# Run tests
test: generate fmt vet manifests
	go test -race ./api/... ./controllers/... ./pkg/... -coverprofile=coverage.txt -covermode=atomic


# Build manager binary
manager: generate fmt vet
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/konsumerator main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet
	go run ./main.go

# Install CRDs into a cluster
install: manifests
	kubectl apply -f config/crd/bases

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	kubectl apply -f config/crd/bases
	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt,year=2019 paths=./api/...

# Build the docker image
docker-build: build test
	docker build . -t ${IMG}
	@echo "updating kustomize image patch file for manager resource"
	sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/default/manager_image_patch.yaml

# Push the docker image
docker-push:
	docker push ${IMG}

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.2.0-beta.2
CONTROLLER_GEN=$(shell go env GOPATH)/bin/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

kind-destroy:
	-kind delete cluster --name "konsumerator"

kind-create: docker-build kind-destroy
	kind create cluster --name "konsumerator" --config ./hack/ci/kind.yaml
	make kind-load-image
	KUBECONFIG=$$(kind get kubeconfig-path --name="konsumerator") make kind-apply
	KUBECONFIG=$$(kind get kubeconfig-path --name="konsumerator") make deploy

kind-load-image:
	kind load docker-image --name "konsumerator" ${IMG}

kind-update: docker-build kind-load-image deploy

kind-apply:
	kubectl apply -f ./hack/ci/prom.yaml -n kube-system
	kubectl apply -f ./hack/ci/konsumerator-dashboard.yaml -n kube-system
	kubectl apply -f ./hack/ci/grafana.yaml -n kube-system

