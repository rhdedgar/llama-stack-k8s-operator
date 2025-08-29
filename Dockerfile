# Build the manager binary
ARG GOLANG_VERSION=1.24

FROM registry.access.redhat.com/ubi9/go-toolset:${GOLANG_VERSION} as builder
ARG TARGETOS=linux
ARG TARGETARCH
ARG CGO_ENABLED=1
ARG GOTAGS=strictfipsruntime,openssl
ENV GOEXPERIMENT=strictfipsruntime

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY distributions.json distributions.json

# Build the manager binary
USER root

# GOARCH is intentionally left empty to automatically detect the host architecture
# This ensures the binary matches the platform where image-build is executed
RUN CGO_ENABLED=${CGO_ENABLED} GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -tags=${GOTAGS} -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/controllers/manifests ./manifests/
COPY --from=builder /usr/bin/openssl /usr/bin/openssl
COPY --from=builder /lib64/libssl.so.3 /lib64/libssl.so.3
COPY --from=builder /lib64/libcrypto.so.3 /lib64/libcrypto.so.3
USER 1001

ENTRYPOINT ["/manager"]
