# Build the Praesto CSI node driver binary.
# Use BUILDPLATFORM to run the build on the native architecture and cross-compile for TARGETPLATFORM.
FROM --platform=$BUILDPLATFORM golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy the Go Modules manifests first to cache dependencies.
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

# Copy the source tree (relies on .dockerignore to filter unnecessary files).
COPY . .

# Build the CSI node driver for the target platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -o csi-node-driver ./cmd/csi-node-driver

# Use distroless as minimal base image to package the CSI node driver binary.
# The CSI node plugin is expected to run privileged as root so it can create its
# kubelet plugin socket and perform mount operations.
FROM gcr.io/distroless/static
WORKDIR /
COPY --from=builder /workspace/csi-node-driver .

ENTRYPOINT ["/csi-node-driver"]
