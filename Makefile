BIN := wf
PKGS := ./cmd/... ./internal/...
# The stamp exists because a run against a stale installed binary looks exactly
# like a run against the new one; freeze.json records this value.
VERSION := $(shell git describe --always --dirty 2>/dev/null || echo unknown)

.PHONY: build test lint fmt install clean check-tools

build:
	go build -ldflags '-X main.version=$(VERSION)' -o $(BIN) ./cmd/wf

test:
	go test $(PKGS) -race -count=1

lint:
	go vet $(PKGS)
	@out=$$(gofmt -l cmd internal); if [ -n "$$out" ]; then echo "gofmt:"; echo "$$out"; exit 1; fi
	@if command -v golangci-lint >/dev/null; then golangci-lint run; \
	else echo "golangci-lint absent, skipped"; fi

fmt:
	gofmt -w cmd internal

# This repo is the single source of truth for the whole gate toolchain. The wf
# binary is COPIED (a build artifact; freeze.json records its stamp, verify
# notes a stale one). The scripts and the two spec documents are SYMLINKED: a
# second copy of a spec is this project's most-repeated defect class, and a
# stale interpreted script is how the P2e false green shipped. Consumer paths
# (~/.local/bin, ~/.claude) do not change — only what stands behind them.
# install covers what the PLUGIN cannot: the built binary and the two script
# symlinks. The skill and the hook ship via the plugin (`scripts/install.sh`
# registers it) — the legacy user-level skill symlink and doc symlinks are
# removed here so the plugin's copy is the only one a session sees.
install: build
	install -m 0755 $(BIN) $(HOME)/.local/bin/$(BIN)
	ln -sfn $(CURDIR)/scripts/wfcheck $(HOME)/.local/bin/wfcheck
	ln -sfn $(CURDIR)/scripts/wfdry $(HOME)/.local/bin/wfdry
	@[ -L $(HOME)/.claude/skills/workflow-gate ] && rm $(HOME)/.claude/skills/workflow-gate || true
	@[ -L $(HOME)/.claude/workflow-guide.md ] && rm $(HOME)/.claude/workflow-guide.md || true
	@[ -L $(HOME)/.claude/workflow-gate-checks.md ] && rm $(HOME)/.claude/workflow-gate-checks.md || true

# The regression the P2e gate paid for, held forever: a body touching the
# harness-only `meta` binding must FAIL every wfdry mode; the clean fixture
# must complete all of them. Plus a parse of both node scripts.
check-tools:
	@node --check scripts/wfcheck && node --check scripts/wfdry && echo "wfcheck/wfdry: parse ok"
	@node scripts/wfdry scripts/testdata/clean.mjs --quiet >/dev/null && echo "wfdry clean fixture: 6/6"
	@if node scripts/wfdry scripts/testdata/meta-guard.mjs --quiet >/dev/null 2>&1; then \
		echo "wfdry meta-guard fixture: FALSE GREEN — the stub resurrected meta"; exit 1; \
	else echo "wfdry meta-guard fixture: fails as it must"; fi

clean:
	rm -f $(BIN)
