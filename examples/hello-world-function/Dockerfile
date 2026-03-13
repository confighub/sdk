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
COPY examples/hello-world-function/go.mod examples/hello-world-function/go.sum ./examples/hello-world-function/

WORKDIR /go/src/app/examples/hello-world-function
RUN go mod download

# Copy source code
WORKDIR /go/src/app
COPY core/ ./core/
COPY configkit/k8skit/ ./configkit/k8skit/
COPY examples/hello-world-function/ ./examples/hello-world-function/

# Build the binary
WORKDIR /go/src/app/examples/hello-world-function
RUN CGO_ENABLED=0 go build -o /go/bin/hello-world-function .

#################
# Runtime stage #
#################
FROM alpine:latest

# Run as non-root
RUN addgroup -g 3000 appgroup && adduser -u 1000 -g appgroup --disabled-password --no-create-home appuser
USER 1000:3000

WORKDIR /app
COPY --from=builder /go/bin/hello-world-function .

ENTRYPOINT ["/app/hello-world-function"]
