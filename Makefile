BIN := wf
PKGS := ./cmd/... ./internal/...

.PHONY: build test lint fmt install clean

build:
	go build -o $(BIN) ./cmd/wf

test:
	go test $(PKGS) -race -count=1

lint:
	go vet $(PKGS)
	@out=$$(gofmt -l cmd internal); if [ -n "$$out" ]; then echo "gofmt:"; echo "$$out"; exit 1; fi
	@command -v golangci-lint >/dev/null && golangci-lint run || echo "golangci-lint absent, skipped"

fmt:
	gofmt -w cmd internal

install: build
	install -m 0755 $(BIN) $(HOME)/.local/bin/$(BIN)

clean:
	rm -f $(BIN)
