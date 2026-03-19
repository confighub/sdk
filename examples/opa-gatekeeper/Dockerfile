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
COPY worker-function-impl/go.mod worker-function-impl/go.sum ./worker-function-impl/
COPY examples/opa-gatekeeper/go.mod examples/opa-gatekeeper/go.sum ./examples/opa-gatekeeper/

WORKDIR /go/src/app/examples/opa-gatekeeper
RUN go mod download

# Copy source code
WORKDIR /go/src/app
COPY core/ ./core/
COPY configkit/k8skit/ ./configkit/k8skit/
COPY worker-function-impl/ ./worker-function-impl/
COPY examples/opa-gatekeeper/ ./examples/opa-gatekeeper/

# Build the binary
WORKDIR /go/src/app/examples/opa-gatekeeper
RUN CGO_ENABLED=0 go build -o /go/bin/opa-gatekeeper .

#################
# Runtime stage #
#################
FROM alpine:latest

# Run as non-root
RUN addgroup -g 3000 appgroup && adduser -u 1000 -g appgroup --disabled-password --no-create-home appuser
USER 1000:3000

WORKDIR /app
COPY --from=builder /go/bin/opa-gatekeeper .

ENTRYPOINT ["/app/opa-gatekeeper"]
