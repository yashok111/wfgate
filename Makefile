BINS := wf wfcheck wfdry
BIN := wf
PKGS := ./cmd/... ./internal/...
# The stamp exists because a run against a stale installed binary looks exactly
# like a run against the new one; freeze.json records this value.
VERSION := $(shell git describe --always --dirty 2>/dev/null || echo unknown)

.PHONY: build test lint fmt install clean check-tools check-docs

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
	@if ./wfcheck -root . testdata/own-forbid.mjs >/dev/null 2>&1; then \
		echo "wfcheck own-forbid fixture: LOST a dual-ownership ERROR beside a forbidding phrase"; exit 1; \
	else echo "wfcheck own-forbid fixture: reports the collision as it must"; fi
	@if ./wfdry testdata/dead-reviews-green.mjs --quiet >/dev/null 2>&1; then \
		echo "wfdry dead-reviews fixture: EXIT 0 over a run that reviewed NOTHING and called itself green"; exit 1; \
	else echo "wfdry dead-reviews fixture: fails as it must"; fi
	@if ./wfdry testdata/collapse.mjs --quiet >/dev/null 2>&1; then \
		echo "wfdry collapse fixture: EXIT 0 over modes that reached a sixth of the script"; exit 1; \
	else echo "wfdry collapse fixture: fails as it must"; fi
	@if ./wfdry testdata/section-collapse.mjs --quiet >/dev/null 2>&1; then \
		echo "wfdry section-collapse fixture: EXIT 0 over modes that never reached a whole section"; exit 1; \
	else echo "wfdry section-collapse fixture: fails as it must"; fi
	@./wfdry testdata/section-collapse.mjs --quiet 2>&1 | grep -q 'the phase(s) `S2`' \
		&& echo "wfdry section-collapse fixture: NAMES the phase no failure mode reached" \
		|| { echo "wfdry section-collapse fixture: said a phase was missed and did not say WHICH — the operator diffs two runs by hand"; exit 1; }
	@if ./wfdry testdata/collapse.mjs --quiet 2>&1 | grep -q "(clean: 6 agents)"; then \
		echo "wfdry collapse fixture: prints the happy path's count beside each mode"; \
	else echo "wfdry collapse fixture: no baseline printed — the operator has nothing to compare a mode against"; exit 1; fi
	@rm -rf .check-skel && mkdir -p .check-skel && ./wf new-script skeleton -target . -o .check-skel/skeleton.mjs >/dev/null \
		&& ./wfcheck -root . .check-skel/skeleton.mjs >/dev/null \
		&& ./wfdry .check-skel/skeleton.mjs --quiet >/dev/null \
		&& rm -rf .check-skel \
		&& echo "wf new-script skeleton: wfcheck clean and wfdry all modes"
	@rm -rf .check-skel && mkdir -p .check-skel && ./wf new-script skeleton -target . -o .check-skel/skeleton.mjs >/dev/null; \
		c=$$(./wfdry .check-skel/skeleton.mjs --mode blockers 2>/dev/null | awk '/"carried": \[/{f=1} f{print} f&&/^   \],?$$/{exit}'); \
		rm -rf .check-skel; \
		for src in review recheck accept; do \
			echo "$$c" | grep -q "from $$src" || { \
				echo "wf new-script skeleton: the $$src step's non-blocking findings reach NO bucket — a major is a real defect by this repo's own scale and it would survive the run as nothing"; exit 1; }; \
		done; \
		echo "wf new-script skeleton: review, recheck and accept all CARRY their non-blocking findings"
	@./wfcheck -root . testdata/conforming.mjs >/dev/null && echo "wfcheck conforming fixture: no errors on a script written to the manual"
	@./wfdry testdata/prompt-drift.mjs --quiet --prompt-diff accept:reads,verify 2>/dev/null \
		| grep -q "only .accept:reads.: A clause you believe is true" \
		&& echo "wfdry prompt-drift fixture: names the rule stated to one prompt and not the other" \
		|| { echo "wfdry prompt-drift fixture: the diff MISSED a governing sentence given to one of two agents doing one job"; exit 1; }
	@out=$$(./wfdry testdata/prompt-drift.mjs --quiet --prompt-diff accept:reads 2>&1); rc=$$?; \
		[ $$rc = 2 ] && echo "$$out" | grep -q "takes exactly two labels" \
		&& echo "wfdry prompt-drift fixture: --prompt-diff refuses anything but two labels" \
		|| { echo "wfdry prompt-drift fixture: ONE label must exit 2 with the usage error, got rc=$$rc: $$out"; exit 1; }
	@out=$$(./wfdry testdata/clean.mjs --quiet --dead wroker 2>&1); rc=$$?; \
		[ $$rc = 1 ] && echo "$$out" | grep -q "matched NO spawn in any mode" \
		&& echo "wfdry --dead: a label that spawns in NO mode fails the run" \
		|| { echo "wfdry --dead: a mistyped label ran every guard over the empty set and exited $$rc"; exit 1; }
	@./wfdry testdata/clean.mjs --quiet --dead worker >/dev/null 2>&1 \
		&& echo "wfdry --dead: a label that DOES spawn is silent" \
		|| { echo "wfdry --dead: a correct label failed the run"; exit 1; }
	@out=$$(./wfdry testdata/prompt-drift.mjs --quiet --prompt-diff accept:reads,accept:reads 2>&1); rc=$$?; \
		[ $$rc = 2 ] && echo "$$out" | grep -q "same prompt" \
		&& echo "wfdry prompt-drift fixture: two spellings of ONE prompt are refused, not reported as agreement" \
		|| { echo "wfdry prompt-drift fixture: a text diffed against ITSELF reported agreement, got rc=$$rc: $$out"; exit 1; }
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

# The repository's own manual is a gate corpus like any other, and the two
# checks that read prose had never been run over it. Measured 2026-08-27 over
# the shipped set (README + SKILL + the six references): `wf check rules`
# reported 2 conflict(s) — [R2] and [R3] marked on POINTERS inside one README
# command cell, which is the class the ninth wave banked a rule against,
# standing in the manual that ships the check. The rules half is a BAR because
# its population is small and every conflict it has ever printed here was real.
# The dup half is printed as a POPULATION and never fails: a reference quoting
# a rule verbatim is not drift, and a bar that fails on that is `dup`'s own
# 161-groups-of-176 lesson repeated.
#
# Two assertions guard the rig itself. An empty artifact set makes `wf check
# rules` refuse, but an artifact set that carries no markers exits ZERO under a
# header reading "NOT A PASS, nothing was compared" — so a copy step that
# silently narrowed the corpus would read as a green bar. The file count is
# compared against the source list for the same reason: a reference renamed out
# of the glob takes its rules with it and nothing else would say so.
check-docs: build
	@src="README.md skills/workflow-gate/SKILL.md $$(ls skills/workflow-gate/references/*.md)"; \
	want=$$(echo $$src | wc -w); \
	d=$$(mktemp -d); \
	( cd $$d && $(CURDIR)/wf init docs -target $(CURDIR) >/dev/null ) || { echo "check-docs: could not build the scratch workspace"; rm -rf $$d; exit 1; }; \
	cp $$src $$d/docs/artifacts/ || { echo "check-docs: could not stage the manual"; rm -rf $$d; exit 1; }; \
	got=$$(ls -1 $$d/docs/artifacts/*.md 2>/dev/null | wc -l); \
	[ "$$got" = "$$want" ] || { echo "check-docs: staged $$got of $$want document(s) — the corpus is not the manual"; rm -rf $$d; exit 1; }; \
	( cd $$d/docs && $(CURDIR)/wf add artifacts/*.md >/dev/null ) || { echo "check-docs: could not commit the corpus"; rm -rf $$d; exit 1; }; \
	out=$$(cd $$d/docs && $(CURDIR)/wf check rules 2>&1); rc=$$?; \
	echo "$$out" | head -1; \
	case "$$out" in *"NOT A PASS"*) echo "check-docs: the rules check compared NOTHING — an absent measurement, not a pass"; rm -rf $$d; exit 1;; esac; \
	[ $$rc = 0 ] || { echo "$$out"; echo "check-docs: the shipped manual disagrees with itself — see the sites above"; rm -rf $$d; exit 1; }; \
	( cd $$d/docs && $(CURDIR)/wf check dup 2>&1 | head -1 ); \
	echo "check-docs: the manual agrees with itself across $$want document(s)"; \
	rm -rf $$d

clean:
	rm -f $(BINS)
