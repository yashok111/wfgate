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
	@if ./wfcheck -root . testdata/one-error.mjs >/dev/null 2>&1; then \
		echo "wfcheck one-error fixture: EXIT 0 over an ERROR — the exit code is decorative"; exit 1; \
	else echo "wfcheck one-error fixture: exits non-zero as it must"; fi
	@if ./wfdry testdata/zero-spawn.mjs --quiet >/dev/null 2>&1; then \
		echo "wfdry zero-spawn fixture: EXIT 0 over a run that spawned nothing"; exit 1; \
	else echo "wfdry zero-spawn fixture: fails as it must"; fi
	@if ./wfdry testdata/false-green.mjs --quiet >/dev/null 2>&1; then \
		echo "wfdry false-green fixture: EXIT 0 over a happy path that returned not-green"; exit 1; \
	else echo "wfdry false-green fixture: fails as it must"; fi
	@rm -rf .check-skel && mkdir -p .check-skel && ./wf new-script skeleton -target . -o .check-skel/skeleton.mjs >/dev/null \
		&& ./wfcheck -root . .check-skel/skeleton.mjs >/dev/null \
		&& ./wfdry .check-skel/skeleton.mjs --quiet >/dev/null \
		&& rm -rf .check-skel \
		&& echo "wf new-script skeleton: wfcheck clean and wfdry all modes"
	@./wfcheck -root . testdata/conforming.mjs >/dev/null && echo "wfcheck conforming fixture: no errors on a script written to the manual"
	@d=$$(mktemp -d); printf '{"session_id":"mk","tool_name":"Write","tool_input":{"file_path":"/tmp/notes.md","content":"prose"}}' \
		| TMPDIR=$$d sh scripts/hooks/script-authoring.sh | grep -q . \
		&& { echo "authoring hook: fired on an ordinary file — a reminder on every write is a reminder nobody reads"; rm -rf $$d; exit 1; } \
		|| { echo "authoring hook: silent on an ordinary file"; rm -rf $$d; }
	@d=$$(mktemp -d); printf '{"session_id":"mk","tool_name":"Write","tool_input":{"file_path":"/tmp/run.mjs","content":"x"}}' \
		| TMPDIR=$$d sh scripts/hooks/script-authoring.sh | grep -q "MANDATORY BEFORE authoring" \
		&& { echo "authoring hook: fires on an orchestration script"; } \
		|| { echo "authoring hook: SILENT on a script — the authoring phase has no deterministic trigger again"; rm -rf $$d; exit 1; }; \
		printf '{"session_id":"mk","tool_name":"Write","tool_input":{"file_path":"/tmp/again.mjs","content":"x"}}' \
		| TMPDIR=$$d sh scripts/hooks/script-authoring.sh | grep -q . \
		&& { echo "authoring hook: fired TWICE in one session — repeated, it stops being read"; rm -rf $$d; exit 1; } \
		|| { echo "authoring hook: once per session"; rm -rf $$d; }

clean:
	rm -f $(BINS)
