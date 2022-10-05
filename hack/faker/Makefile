VERSION?=$(shell git rev-list --count HEAD)-$(shell git rev-parse --short=12 HEAD)
IMAGE?=lwolf/faker
deploy: redis
	docker build -t ${IMAGE}:${VERSION} .
	kind load docker-image --name "konsumerator" docker.io/${IMAGE}:${VERSION}
	$(MAKE) producer
	$(MAKE) consumer

redis:
	kubectl apply --wait=true -f deploy/redis.yaml

producer:
	sed 's/faker:latest/faker:${VERSION}/g' deploy/deploy.yaml | kubectl apply  --wait=true -f -

consumer:
	sed 's/faker:latest/faker:${VERSION}/g' ../ci/consumer-test.yaml | kubectl apply -f -

consumer-guest:
	sed 's/faker:latest/faker:${VERSION}/g' ../ci/consumer-test-configmap.yaml | kubectl apply -f -

build:
	go build -o bin/faker main.go

docker-build:
	docker build -t ${IMAGE}:${VERSION} .