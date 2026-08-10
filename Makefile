.PHONY: test build dist-macos install clean

BINARY := astock
LDFLAGS := -s -w -buildid=

test:
	go test ./...
	go vet ./...

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

clean:
	rm -f dist/$(BINARY) dist/$(BINARY)-darwin-amd64 dist/$(BINARY)-darwin-arm64 dist/$(BINARY)-darwin-universal
