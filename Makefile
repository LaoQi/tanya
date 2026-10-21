VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_TIME := $(shell date '+%Y-%m-%d %H:%M')

CROSS_GOOS := darwin windows

build:
	go build -ldflags "-X main.version=$(VERSION) -X 'main.buildTime=$(BUILD_TIME)'" -o tanya .

cross:
	@set -e; for os in $(CROSS_GOOS); do \
		echo "== GOOS=$$os go vet ./..."; \
		GOOS=$$os go vet ./...; \
		echo "== GOOS=$$os go test -c（逐包，仅编译不运行）"; \
		for p in $$(go list ./...); do \
			GOOS=$$os go test -c -o /dev/null $$p; \
		done; \
	done
	@echo "cross: OK"

.PHONY: build cross
