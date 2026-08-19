.PHONY: test web-build build dist-macos install tdx-setup clean

BINARY := astock
LDFLAGS := -s -w -buildid=
TDX_VENV ?= $(HOME)/.local/share/astock-workbench/tdx-venv

test:
	go test ./...
	go vet ./...

web-build:
	cd web && npm run build

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags='$(LDFLAGS)' -o dist/$(BINARY) ./cmd/astock

dist-macos:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags='$(LDFLAGS)' -o dist/$(BINARY)-darwin-amd64 ./cmd/astock
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags='$(LDFLAGS)' -o dist/$(BINARY)-darwin-arm64 ./cmd/astock
	codesign --force --sign - dist/$(BINARY)-darwin-amd64
	codesign --force --sign - dist/$(BINARY)-darwin-arm64
	lipo -create dist/$(BINARY)-darwin-amd64 dist/$(BINARY)-darwin-arm64 -output dist/$(BINARY)-darwin-universal
	codesign --force --sign - dist/$(BINARY)-darwin-universal

install: build
	install -m 0755 dist/$(BINARY) /usr/local/bin/$(BINARY)

tdx-setup:
	uv venv $(TDX_VENV) --python 3.11
	uv pip install --python $(TDX_VENV)/bin/python tdxrs==0.6.7
	@echo "TDX Python ready: $(TDX_VENV)/bin/python"

clean:
	rm -f dist/$(BINARY) dist/$(BINARY)-darwin-amd64 dist/$(BINARY)-darwin-arm64 dist/$(BINARY)-darwin-universal
