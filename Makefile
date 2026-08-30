.PHONY: build run test vet fmt tidy clean install help
.PHONY: release

BINARY_NAME=gitscan
BINARY_PATH=$(BINARY_NAME)
INSTALL_PATH=$(HOME)/.local/bin/$(BINARY_NAME)
VERSION?=v0.1.0
PKG=github.com/aognio/gitscan/cmd
LDFLAGS=-X $(PKG).Version=$(VERSION)

GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOVET=$(GOCMD) vet
GOMOD=$(GOCMD) mod

help:
	@echo "gitscan — Makefile targets:"
	@echo ""
	@echo "  build       Build the binary to $(BINARY_PATH) (bare)"
	@echo "  release     Build with version injected (VERSION=$(VERSION))"
	@echo "  run         Build and run gitscan scan"
	@echo "  test        Run tests"
	@echo "  vet         Run go vet"
	@echo "  fmt         Format code (gofumpt, falls back to gofmt)"
	@echo "  tidy        Run go mod tidy"
	@echo "  clean       Remove build artifacts"
	@echo "  install     Install binary to $(INSTALL_PATH)"
	@echo ""
	@echo "Example: make release VERSION=v0.2.0"
	@echo ""

build:
	@echo "Building $(BINARY_NAME)..."
	$(GOBUILD) -o $(BINARY_PATH) ./cmd/gitscan
	@echo "Built: $(BINARY_PATH)"

release:
	@echo "Building $(BINARY_NAME) $(VERSION)..."
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BINARY_PATH) ./cmd/gitscan
	@echo "Built: $(BINARY_PATH) ($(VERSION))"

run: build
	@echo "Running $(BINARY_NAME)..."
	@./$(BINARY_PATH) scan --plain --root $(HOME)/code

test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

vet:
	@echo "Vetting..."
	$(GOVET) ./...

fmt:
	@echo "Formatting..."
	@command -v gofumpt >/dev/null 2>&1 && gofumpt -w . || gofmt -s -w .

tidy:
	@echo "Tidying modules..."
	$(GOMOD) tidy

clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	rm -rf bin/
	@echo "Cleaned"

install: build
	@echo "Installing $(BINARY_NAME) to $(INSTALL_PATH)..."
	@mkdir -p $(dir $(INSTALL_PATH))
	install -D -m 755 $(BINARY_PATH) $(INSTALL_PATH)
	@echo "Installed"