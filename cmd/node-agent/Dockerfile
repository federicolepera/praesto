# Build the Praesto node-agent binary.
# Use BUILDPLATFORM to run the build on the native architecture and cross-compile for TARGETPLATFORM.
FROM --platform=$BUILDPLATFORM golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -o node-agent ./cmd/node-agent

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/node-agent .
USER 0:0

ENTRYPOINT ["/node-agent"]
