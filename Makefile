BINARY_CLI  := unbound-dns
BINARY_GUI  := unbound-dns-gui
DIST        := dist
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -X main.version=$(VERSION)

# The CLI stays free of cgo so it cross-compiles with a single command. The GUI
# needs cgo for Fyne, so it is built per platform.
CLI_PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build build-cli build-gui cross-cli test test-race lint vuln fmt tidy clean

all: build

build: build-cli

build-cli:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY_CLI) ./cmd/$(BINARY_CLI)

build-gui:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY_GUI) ./cmd/$(BINARY_GUI)

cross-cli:
	@for platform in $(CLI_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		echo "  $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o $(DIST)/$(BINARY_CLI)-$$os-$$arch$$ext ./cmd/$(BINARY_CLI) || exit 1; \
	done

# -count=1 disables the test cache so every run actually executes.
test:
	go test -count=1 ./...

test-race:
	go test -count=1 -race ./...

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
