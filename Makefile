.DEFAULT_GOAL := help

GO ?= go

include ./test/integration-test.mk

help:
	@echo "make fmt                  format source code"
	@echo "make verify               verify source code"
	@echo "make build                compile binaries"
	@echo "make test                 run unit tests"
	@echo "make test-integration     run integration tests"
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

clean:
	go clean
	rm -rf $(BIN_DIR)
	rm -f ./integration.test
.PHONY: clean
