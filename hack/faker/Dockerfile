FROM golang:1.19 as builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /work
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY cmd/ cmd/
COPY lib/ lib/

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -a -o faker main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /work/faker /
USER 65532:65532

ENTRYPOINT ["/faker"]
