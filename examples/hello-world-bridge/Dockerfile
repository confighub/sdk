# Copyright (C) ConfigHub, Inc.
# SPDX-License-Identifier: MIT

###############
# Build stage #
###############
FROM golang:1.25-alpine AS builder
WORKDIR /go/src/app

# Cache dependency downloads in separate layers
COPY core/go.mod core/go.sum ./core/
COPY examples/hello-world-bridge/go.mod examples/hello-world-bridge/go.sum ./examples/hello-world-bridge/

WORKDIR /go/src/app/examples/hello-world-bridge
RUN go mod download

# Copy source code
WORKDIR /go/src/app
COPY core/ ./core/
COPY examples/hello-world-bridge/ ./examples/hello-world-bridge/

# Build the binary
WORKDIR /go/src/app/examples/hello-world-bridge
RUN CGO_ENABLED=0 go build -o /go/bin/hello-world-bridge .

#################
# Runtime stage #
#################
FROM alpine:latest

# Run as non-root
RUN addgroup -g 3000 appgroup && adduser -u 1000 -g appgroup --disabled-password --no-create-home appuser
USER 1000:3000

WORKDIR /app
COPY --from=builder /go/bin/hello-world-bridge .

ENTRYPOINT ["/app/hello-world-bridge"]
