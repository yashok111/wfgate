BINS := wf wfcheck wfdry
BIN := wf
PKGS := ./cmd/... ./internal/...
# The stamp exists because a run against a stale installed binary looks exactly
# like a run against the new one; freeze.json records this value.
VERSION := $(shell git describe --always --dirty 2>/dev/null || echo unknown)

.PHONY: build test lint fmt install clean check-tools

build:
	@for b in $(BINS); do \
		go build -ldflags '-X main.version=$(VERSION)' -o $$b ./cmd/$$b || exit 1; \
	done

test:
	go test $(PKGS) -race -count=1

lint:
	go vet $(PKGS)
	@out=$$(gofmt -l cmd internal); if [ -n "$$out" ]; then echo "gofmt:"; echo "$$out"; exit 1; fi
	@if command -v golangci-lint >/dev/null; then golangci-lint run; \
	else echo "golangci-lint absent, skipped"; fi

fmt:
	gofmt -w cmd internal

# This repo is the single source of truth for the whole gate toolchain. All
# three binaries are COPIED, and all three carry the same version stamp: a run
# against a stale installed copy is indistinguishable from a run against the
# new one, which is why freeze.json records the stamp and verify notes a
# mismatch. wfcheck and wfdry used to be interpreted scripts SYMLINKED into
# place; they are compiled now, so the symlink that kept them fresh is gone
# with them. install covers what the PLUGIN cannot: the built binaries. The
# skill and the hook ship via the plugin (`scripts/install.sh` registers it) —
# the legacy user-level skill symlink and doc symlinks are removed here so the
# plugin's copy is the only one a session sees.
install: build
	@[ -L $(HOME)/.local/bin/wfcheck ] && rm $(HOME)/.local/bin/wfcheck || true
	@[ -L $(HOME)/.local/bin/wfdry ] && rm $(HOME)/.local/bin/wfdry || true
	@for b in $(BINS); do install -m 0755 $$b $(HOME)/.local/bin/$$b || exit 1; done
	@[ -L $(HOME)/.claude/skills/workflow-gate ] && rm $(HOME)/.claude/skills/workflow-gate || true
	@[ -L $(HOME)/.claude/workflow-guide.md ] && rm $(HOME)/.claude/workflow-guide.md || true
	@[ -L $(HOME)/.claude/workflow-gate-checks.md ] && rm $(HOME)/.claude/workflow-gate-checks.md || true

# The regression the P2e gate paid for, held forever, now driven through the
# BUILT binaries rather than the packages: a body touching the harness-only
# `meta` binding must FAIL every wfdry mode, and the clean fixture must complete
# all of them. The package tests cover each guard one by one; this is the
# end-to-end statement that the thing an operator runs behaves that way.
check-tools: build
	@./wfdry testdata/clean.mjs --quiet >/dev/null && echo "wfdry clean fixture: all modes"
	@if ./wfdry testdata/meta-guard.mjs --quiet >/dev/null 2>&1; then \
		echo "wfdry meta-guard fixture: FALSE GREEN — the stub resurrected meta"; exit 1; \
	else echo "wfdry meta-guard fixture: fails as it must"; fi
	@./wfcheck -root . testdata/clean.mjs >/dev/null && echo "wfcheck clean fixture: no errors"

clean:
	rm -f $(BINS)
