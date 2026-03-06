TEST_TMP :=/tmp

export KUBEBUILDER_ASSETS ?=$(TEST_TMP)/kubebuilder/bin

GO ?=go
GOHOSTOS ?=$(shell $(GO) env GOHOSTOS)
GOHOSTARCH ?=$(shell $(GO) env GOHOSTARCH)

K8S_VERSION ?=1.30.0
KB_TOOLS_ARCHIVE_NAME :=kubebuilder-tools-$(K8S_VERSION)-$(GOHOSTOS)-$(GOHOSTARCH).tar.gz
KB_TOOLS_ARCHIVE_PATH := $(TEST_TMP)/$(KB_TOOLS_ARCHIVE_NAME)

ENSURE_ENVTEST_SCRIPT := https://raw.githubusercontent.com/open-cluster-management-io/sdk-go/main/ci/envtest/ensure-envtest.sh

envtest-setup:
	$(eval export KUBEBUILDER_ASSETS=$(shell curl -fsSL $(ENSURE_ENVTEST_SCRIPT) | bash))
	@echo "KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS)"
.PHONY: envtest-setup

clean-integration-test:
	$(RM) '$(KB_TOOLS_ARCHIVE_PATH)'
	rm -rf $(TEST_TMP)/kubebuilder
	$(RM) ./*integration.test
.PHONY: clean-integration-test

clean: clean-integration-test

build-integration:
	go test -c ./test/integration -o ./integration.test
.PHONY: build-integration

test-integration: envtest-setup build-integration
	./integration.test --ginkgo.slow-spec-threshold=15s --ginkgo.v --ginkgo.fail-fast ${ARGS}
.PHONY: test-integration
