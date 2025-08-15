.DEFAULT_GOAL := help

GO ?= go
container_tool ?= podman
image_repository ?= quay.io/stolostron
image_name ?= cloudevents-conductor
version:=$(shell date +%s)
image_tag ?= $(version)

include ./test/integration-test.mk

help:
	@echo "make fmt                  format source code"
	@echo "make verify               verify source code"
	@echo "make build                compile binaries"
	@echo "make test                 run unit tests"
	@echo "make test-integration     run integration tests"
	@echo "make test-e2e"            run end-to-end test
	@echo "make image"				 build container image"
	@echo "make push"                push container image"
	@echo "make clean                delete temporary generated files"
.PHONY: help

fmt:
	${GO} fmt \
		./cmd/... \
		./pkg/...
.PHONY: fmt

verify:
	${GO} vet \
		./cmd/... \
		./pkg/...
.PHONY: verify

BIN_DIR := bin
BIN := $(BIN_DIR)/conductor

build: $(BIN)
.PHONY: build

$(BIN): | $(BIN_DIR)
	${GO} build $(BUILD_OPTS) -o $(BIN) ./cmd

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

test: fmt verify
	${GO} test -coverprofile=coverage.out -v -race \
		./cmd/... \
		./pkg/...
.PHONY: test

test-e2e/setup:
	./test/e2e/e2e_setup.sh
.PHONY: test-e2e/setup

test-e2e/teardown:
	./test/e2e/e2e_teardown.sh
.PHONY: test-e2e/teardown

test-e2e/run:
	ginkgo -v --fail-fast \
	--output-dir="${PWD}/test/e2e/report" \
	--json-report=report.json \
	--junit-report=report.xml \
	${PWD}/test/e2e/pkg -- \
	-maestro-api-server=http://localhost:30080 \
	-maestro-grpc-server=localhost:30090 \
	-hub-kubeconfig=${PWD}/test/e2e/.kubeconfig \
	-managed-kubeconfig=${PWD}/test/e2e/.kubeconfig
.PHONY: test-e2e/run

test-e2e: test-e2e/teardown test-e2e/setup test-e2e/run
.PHONY: test-e2e

image: fmt verify
	$(container_tool) build -f Containerfile -t "$(image_repository)/$(image_name):$(image_tag)" .
.PHONY: image

push: image
	$(container_tool) push "$(image_repository)/$(image_name):$(image_tag)"
.PHONY: push

clean:
	go clean
	rm -rf $(BIN_DIR)
	rm -f ./integration.test
.PHONY: clean
