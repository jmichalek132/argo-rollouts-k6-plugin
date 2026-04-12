MODULE := github.com/jmichalek132/argo-rollouts-k6-plugin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build build-metric build-step test test-e2e test-e2e-live lint lint-stdout clean

build: build-metric build-step

build-metric:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/metric-plugin ./cmd/metric-plugin

build-step:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/step-plugin ./cmd/step-plugin

test:
	go test -race -v -count=1 ./...

test-e2e:
	GOPATH="$(HOME)/go" DOCKER_HOST="unix://$(HOME)/.colima/default/docker.sock" \
	go test -v -tags=e2e -count=1 -timeout=15m ./e2e/...

test-e2e-live:
	GOPATH="$(HOME)/go" DOCKER_HOST="unix://$(HOME)/.colima/default/docker.sock" \
	K6_LIVE_TEST=true \
	K6_CLOUD_TOKEN=$(K6_CLOUD_TOKEN) \
	K6_STACK_ID=$(K6_STACK_ID) \
	K6_TEST_ID=$(K6_TEST_ID) \
	K6_FAILING_TEST_ID=$(K6_FAILING_TEST_ID) \
	go test -v -tags=e2e -count=1 -timeout=30m ./e2e/...

lint: lint-stdout
	golangci-lint run

lint-stdout:
	@echo "Checking for stdout usage in non-test code..."
	@if grep -rn 'fmt\.Print\|fmt\.Fprint.*os\.Stdout\|os\.Stdout' cmd/ internal/ --include='*.go' | grep -v '_test.go' | grep -v '// stdout-ok'; then \
		echo "ERROR: found stdout usage in non-test code (stdout reserved for go-plugin handshake)"; \
		exit 1; \
	fi
	@echo "No stdout usage found -- OK"

clean:
	rm -rf bin/
