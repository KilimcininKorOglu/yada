BINARY      := unbound-dns
DIST        := dist
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -X main.version=$(VERSION)

# The full binary opens the desktop interface by default and drops to the CLI
# with -cli. It links Fyne, so it needs cgo and is built per platform.
#
# The nogui build tag strips the interface, which leaves a static binary that
# cross-compiles to every platform with one command. That is the build for
# servers and CI.
CLI_PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

DOCKER_COMPOSE := docker compose -f docker/docker-compose.yml

.PHONY: all build build-cli cross-cli cross-gui test test-race test-nogui lint vuln fmt tidy clean \
	docker-keys docker-up docker-down docker-logs docker-test

all: build

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) ./cmd/$(BINARY)

build-cli:
	CGO_ENABLED=0 go build -trimpath -tags nogui -ldflags "$(LDFLAGS)" \
		-o $(DIST)/$(BINARY)-cli ./cmd/$(BINARY)

cross-cli:
	@for platform in $(CLI_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		echo "  $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -tags nogui \
			-ldflags "$(LDFLAGS)" \
			-o $(DIST)/$(BINARY)-$$os-$$arch$$ext ./cmd/$(BINARY) || exit 1; \
	done

# Builds the interface for every platform from one macOS machine.
#
# CI does not need this: it builds on a native runner per platform. It exists
# for producing a full set locally, which needs a C toolchain per target.
#
#   brew install FiloSottile/musl-cross/musl-cross mingw-w64
#
# Linux is built inside a container rather than cross-compiled, because glfw
# needs the target's X11, Wayland and OpenGL headers and no cross toolchain
# ships them. linux/amd64 runs under emulation on Apple Silicon, so it is slow.
cross-gui:
	@echo "  darwin/arm64"
	@CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(DIST)/$(BINARY)-gui-darwin-arm64 ./cmd/$(BINARY)
	@echo "  darwin/amd64"
	@CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC="clang -arch x86_64" CXX="clang++ -arch x86_64" \
		go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(DIST)/$(BINARY)-gui-darwin-amd64 ./cmd/$(BINARY)
	@echo "  windows/amd64"
	@CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
		go build -trimpath -ldflags "$(LDFLAGS) -H windowsgui" \
		-o $(DIST)/$(BINARY)-gui-windows-amd64.exe ./cmd/$(BINARY)
	@for arch in arm64 amd64; do \
		echo "  linux/$$arch (konteyner)"; \
		docker run --rm --platform linux/$$arch \
			-e VERSION="$(VERSION)" -e TARGETARCH=$$arch -e GOFLAGS=-buildvcs=false \
			-v "$(CURDIR)":/src:ro -v "$(CURDIR)/$(DIST)":/out \
			-v "$(CURDIR)/scripts/build-gui-linux.sh":/build.sh:ro \
			golang:1.26 sh /build.sh || exit 1; \
	done

# -count=1 disables the test cache so every run actually executes.
test:
	go test -count=1 ./...

test-race:
	go test -count=1 -race ./...

# The same suite without the GUI package, for a machine with no OpenGL headers.
test-nogui:
	CGO_ENABLED=0 go test -count=1 -tags nogui ./...

lint:
	golangci-lint run ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

clean:
	rm -rf $(DIST)

# --- Test environment ------------------------------------------------------
#
# Three Unbound servers reachable over ssh. See docker/README.md.

docker-keys:
	@docker/make-keys.sh

docker-up: docker-keys
	$(DOCKER_COMPOSE) up -d --build
	@docker/wait-ready.sh

docker-down:
	$(DOCKER_COMPOSE) down -v

docker-logs:
	$(DOCKER_COMPOSE) logs -f

# End-to-end run of the CLI against the containers.
docker-test: build-cli docker-up
	@docker/integration-test.sh
