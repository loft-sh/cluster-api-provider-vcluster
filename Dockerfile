# Build the manager binary
FROM golang:1.23 as builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Install Delve for debugging
RUN if [ "${TARGETARCH}" = "amd64" ]; then go install github.com/go-delve/delve/cmd/dlv@latest; fi

# Install Helm 3
RUN curl -s https://get.helm.sh/helm-v3.11.1-linux-amd64.tar.gz > helm3.tar.gz \
 && tar -zxvf helm3.tar.gz linux-amd64/helm \
 && chmod +x linux-amd64/helm \
 && mv linux-amd64/helm $PWD/helm \
 && rm helm3.tar.gz \
 && rm -R linux-amd64

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

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/helm .
USER 65532:65532

ENTRYPOINT ["/manager"]
