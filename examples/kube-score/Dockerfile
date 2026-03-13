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
COPY examples/kube-score/go.mod examples/kube-score/go.sum ./examples/kube-score/

WORKDIR /go/src/app/examples/kube-score
RUN go mod download

# Copy source code
WORKDIR /go/src/app
COPY core/ ./core/
COPY configkit/k8skit/ ./configkit/k8skit/
COPY examples/kube-score/ ./examples/kube-score/

# Build the binary
WORKDIR /go/src/app/examples/kube-score
RUN CGO_ENABLED=0 go build -o /go/bin/kube-score-worker .

# Install kube-score CLI
RUN go install github.com/zegl/kube-score/cmd/kube-score@latest

#################
# Runtime stage #
#################
FROM alpine:latest

# Run as non-root
RUN addgroup -g 3000 appgroup && adduser -u 1000 -g appgroup --disabled-password --no-create-home appuser
USER 1000:3000

WORKDIR /app
COPY --from=builder /go/bin/kube-score-worker .
COPY --from=builder /go/bin/kube-score /usr/local/bin/kube-score

ENTRYPOINT ["/app/kube-score-worker"]
