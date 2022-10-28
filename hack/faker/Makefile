VERSION?=$(shell git rev-list --count HEAD)-$(shell git rev-parse --short=12 HEAD)
IMAGE?=lwolf/faker
NAMESPACE=konsumerator-system

deploy: redis docker-build load-image producer consumer consumer-guest

redis:
	kubectl apply --wait=true -n ${NAMESPACE} -f deploy/redis.yaml

producer:
	sed 's/faker:latest/faker:${VERSION}/g' deploy/deploy.yaml | kubectl apply  --wait=true -n ${NAMESPACE} -f -

load-image:
	kind load docker-image --name "konsumerator" ${IMAGE}:${VERSION}

consumer:
	sed 's/faker:latest/faker:${VERSION}/g' ../ci/consumer-test.yaml | kubectl apply --wait=true -n ${NAMESPACE} -f -

consumer-guest:
	sed 's/faker:latest/faker:${VERSION}/g' ../ci/consumer-test-configmap.yaml | kubectl apply -n ${NAMESPACE} --wait=true -f -

build:
	go build -o bin/faker main.go

docker-build:
	docker build -t ${IMAGE}:${VERSION} .
