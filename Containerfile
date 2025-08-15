# Copyright (c) 2025 Red Hat, Inc.
# Copyright Contributors to the Open Cluster Management project

# Stage 1: build the target binaries
FROM golang:1.23 AS builder

WORKDIR /workspace
COPY go.sum go.mod ./
COPY ./cmd ./cmd
COPY ./pkg ./pkg

RUN CGO_ENABLED=1 GOFLAGS="-p=4" go build -mod=readonly -tags strictfipsruntime -a -v -o bin/conductor cmd/main.go

# Stage 2: Copy the binaries from the image builder to the base image
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

# Red Hat annotations.
LABEL com.redhat.component="multicluster-cloudevents-conductor"
LABEL org.label-schema.vendor="Red Hat"
LABEL org.label-schema.license="Red Hat Advanced Cluster Management for Kubernetes EULA"
LABEL org.label-schema.schema-version="1.0"

# Bundle metadata
LABEL name="multicluster-hub/multicluster-cloudevents-conductor"
LABEL version="release-1.6"
LABEL summary="multicluster cloudevents conductor"
LABEL io.openshift.expose-services=""
LABEL io.openshift.tags="data,images"
LABEL io.k8s.display-name="multicluster cloudevents conductor"
LABEL io.k8s.description="This is the standard release image for the multicluster cloudevents conductor"
LABEL maintainer="['acm-component-maintainers@redhat.com']"
LABEL description="multicluster cloudevents conductor"

ARG GIT_COMMIT
ENV CONDUCTOR=/server \
  USER_UID=1001 \
  USER_NAME=conductor \
  GIT_COMMIT=${GIT_COMMIT}

# install conductor binary
COPY --from=builder /workspace/bin/conductor ${CONDUCTOR}

USER ${USER_UID}