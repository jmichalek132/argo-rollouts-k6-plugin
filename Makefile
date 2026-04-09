MODULE := github.com/jmichalek132/argo-rollouts-k6-plugin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build build-metric build-step test lint lint-stdout clean

build: build-metric build-step

build-metric:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/metric-plugin ./cmd/metric-plugin

build-step:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/step-plugin ./cmd/step-plugin

test:
	go test -race -v -count=1 ./...

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
