VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_TIME := $(shell date '+%Y-%m-%d %H:%M')

build:
	go build -ldflags "-X main.version=$(VERSION) -X 'main.buildTime=$(BUILD_TIME)'" -o tanya .

.PHONY: build
