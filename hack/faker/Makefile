VERSION?=v0.22
do:
	CGO_ENABLED=0 go build -o bin/faker main.go
	docker build -t lwolf/faker:${VERSION} .
	kind load docker-image --name "konsumerator" lwolf/faker:${VERSION}
	sed 's/faker:latest/faker:${VERSION}/g' deploy/deploy.yaml | kubectl apply -f -
	sed 's/faker:latest/faker:${VERSION}/g' ../ci/consumer-test.yaml | kubectl apply -f -
