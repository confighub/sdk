# Copyright (C) ConfigHub, Inc.
# SPDX-License-Identifier: MIT

###############
# Build stage #
###############
FROM golang:1.25-alpine AS builder
WORKDIR /go/src/app

# Cache dependency downloads in separate layers
COPY core/go.mod core/go.sum ./core/
COPY configkit/k8skit/go.mod configkit/k8skit/go.sum ./configkit/k8skit/
COPY examples/kyverno/go.mod examples/kyverno/go.sum ./examples/kyverno/

WORKDIR /go/src/app/examples/kyverno
RUN go mod download

# Copy source code
WORKDIR /go/src/app
COPY core/ ./core/
COPY configkit/k8skit/ ./configkit/k8skit/
COPY examples/kyverno/ ./examples/kyverno/

# Build the binary
WORKDIR /go/src/app/examples/kyverno
RUN CGO_ENABLED=0 go build -o /go/bin/kyverno-worker .

# Install kyverno CLI from release binary (building from source requires too many deps)
ARG KYVERNO_VERSION=v1.17.1
RUN apk add --no-cache curl && \
    ARCH=$(case "$(uname -m)" in x86_64) echo x86_64;; aarch64) echo arm64;; *) echo "$(uname -m)";; esac) && \
    curl -sLO "https://github.com/kyverno/kyverno/releases/download/${KYVERNO_VERSION}/kyverno-cli_${KYVERNO_VERSION}_linux_${ARCH}.tar.gz" && \
    tar xzf kyverno-cli_*.tar.gz kyverno && \
    mv kyverno /go/bin/kyverno && \
    rm -f kyverno-cli_*.tar.gz

#################
# Runtime stage #
#################
FROM alpine:latest

# Run as non-root
RUN addgroup -g 3000 appgroup && adduser -u 1000 -g appgroup --disabled-password --no-create-home appuser
USER 1000:3000

WORKDIR /app
COPY --from=builder /go/bin/kyverno-worker .
COPY --from=builder /go/bin/kyverno /usr/local/bin/kyverno

ENTRYPOINT ["/app/kyverno-worker"]
