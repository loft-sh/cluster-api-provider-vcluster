# Build the manager binary
FROM ubuntu:22.04 as builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Install curl
RUN apt-get update && apt-get install -y curl

# Install Helm 3
RUN curl -s https://get.helm.sh/helm-v3.17.3-${TARGETOS}-${TARGETARCH}.tar.gz > helm3.tar.gz \
 && tar -zxvf helm3.tar.gz ${TARGETOS}-${TARGETARCH}/helm \
 && chmod +x ${TARGETOS}-${TARGETARCH}/helm \
 && mv ${TARGETOS}-${TARGETARCH}/helm $PWD/helm \
 && rm helm3.tar.gz \
 && rm -R ${TARGETOS}-${TARGETARCH}

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /

COPY release/manager .
COPY --from=builder /workspace/helm .

USER 65532:65532

ENTRYPOINT ["/manager"]
