VERSION?=v0.24
deploy: redis
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/faker main.go
	docker build -t lwolf/faker:${VERSION} .
	kind load docker-image --name "konsumerator" lwolf/faker:${VERSION}
	sed 's/faker:latest/faker:${VERSION}/g' deploy/deploy.yaml | kubectl apply  --wait=true -f -
	$(MAKE) consumer

redis:
	kubectl apply --wait=true -f deploy/redis.yaml

consumer:
	sed 's/faker:latest/faker:${VERSION}/g' ../ci/consumer-test.yaml | kubectl apply -f -
