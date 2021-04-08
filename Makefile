VERSION ?= $(shell git describe --tags --always --dirty="-dev")
# Image URL to use all building/pushing image targets
IMG ?= quay.io/lwolf/konsumerator:$(VERSION)
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true"
KUBECTL ?= "kubectl"

# for some reason travis-ci calls `make` on each step which triggers download
# of all packages. This check disables it.
ifeq ($(CI),true)
all:
	@echo "disabling default target make"
endif


build: manager

# Run tests
test: generate fmt vet manifests
	go test -mod=vendor -race ./api/... ./controllers/... ./pkg/... -coverprofile=coverage.txt -covermode=atomic


# Build manager binary
manager: generate fmt vet
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -ldflags "-X main.Version=${VERSION}" -o bin/konsumerator main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet
	go run -mod=vendor ./main.go --verbose=true

# Install CRDs into a cluster
install: manifests
	${KUBECTL} apply -f config/crd/bases

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	#$(KUBECTL) apply -f config/crd/bases
	kustomize build config/default | $(KUBECTL) apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: bin/controller-gen
	bin/controller-gen $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: bin/controller-gen
	bin/controller-gen object:headerFile=./hack/boilerplate.go.txt,year=2021 paths=./api/...

# Build the docker image
docker-build: build test
	docker build . -t ${IMG}
	@echo "updating kustomize image patch file for manager resource"
	sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/default/manager_image_patch.yaml

# Push the docker image
docker-push:
	docker push ${IMG}

# find or download controller-gen
# download controll -mod=vendorer-gen if necessary
bin/controller-gen:
	go build -mod=vendor -o ./bin/controller-gen "sigs.k8s.io/controller-tools/cmd/controller-gen"

kind-destroy:
	-kind delete cluster --name "konsumerator"

kind-create: docker-build kind-destroy
	kind create cluster --name "konsumerator" --config ./hack/ci/kind.yaml
	make kind-load-image
	make kind-apply
	make deploy

kind-load-image:
	kind load docker-image --name "konsumerator" ${IMG}

kind-update: docker-build kind-load-image deploy

kind-apply:
	$(KUBECTL) --context=kind-konsumerator apply -f ./hack/ci/prom.yaml -n kube-system
	$(KUBECTL) --context=kind-konsumerator apply -f ./hack/ci/konsumerator-dashboard.yaml -n kube-system
	$(KUBECTL) --context=kind-konsumerator apply -f ./hack/ci/konsumerator-overview-dashboard.yaml -n kube-system
	$(KUBECTL) --context=kind-konsumerator apply -f ./hack/ci/grafana.yaml -n kube-system

