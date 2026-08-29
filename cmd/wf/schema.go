package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"git.sumka.site/yakov/wfgate/internal/wfdry"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

// runSchema compares what every agent's output schema OBLIGES its agent to
// fill, between a reference and the working tree.
//
// It exists because of one measured shape, twice. P3d narrowed a schema for the
// width classifier and left a stale band behind; P3e, narrowing one for the same
// reason, lost `required: ['met','evidence']` from the leaf of a twenty-two-key
// report — in a hunk whose own comment was entirely about width, with no warrant
// recorded — and for two rounds a property could come back `{met: true}` with no
// evidence at all, which was the only thing the acceptance gate's evidence
// reviewer had to read. Nothing static reaches it: the leaf is built inside an
// `Object.fromEntries(...map(...))` callback, so `wfcheck` sees no literal, and
// both static tools were clean over that script. The RUNNER holds the composed
// object, and this is the census of it.
//
// The width is printed beside the drop deliberately, because that is the
// correlation the corpus has: both instances happened INSIDE an edit whose
// stated purpose was size. A schema that got narrower and stopped requiring
// things is one edit doing two jobs and saying so about one.
//
// A DROP is the finding. A rise is printed and is not: a schema requiring more
// of its agent is the direction this check exists to protect.
func runSchema(w *ws.Workspace, arts []string, since string) (bool, error) {
	ref := since
	if ref == "" {
		n := newestFrozen(w)
		if n == 0 {
			return false, fmt.Errorf("no frozen round; schema is measured FROM a reference, pass -since or run `wf freeze` first")
		}
		ref = fmt.Sprintf("gate-r%d", n)
	}
	// The reference is resolved ONCE, up front. Without this every artifact
	// failed `ShowFile` and printed "no comparable copy at <ref>" — which is
	// the message for a script ADDED since the reference, so a typo in the ref
	// read exactly like a first review of everything. `wf check closure -round
	// 999` was the same class, and it was a blocker.
	if _, err := w.Git.Run("rev-parse", "--verify", ref+"^{commit}"); err != nil {
		return false, fmt.Errorf("%s is not a revision in this workspace: %w", ref, err)
	}
	// The union of what is here NOW and what was there THEN: enumerating only
	// the current artifacts makes a script DELETED since the reference
	// invisible, and a gate that dropped one of two scripts would read as a
	// gate whose remaining script did not change.
	scripts := scriptSet(arts)
	if was, err := w.Git.FilesAt(ref); err == nil {
		for _, f := range scriptSet(was) {
			if !slices.Contains(scripts, f) {
				scripts = append(scripts, f)
			}
		}
		sort.Strings(scripts)
	}
	fmt.Printf("\n## check schema — against %s\n", ref)
	if len(scripts) == 0 {
		// The population, never a pass. A decision gate has no script and this
		// check has nothing to say about it; a workspace whose script was
		// renamed out of the extensions has the same output and a different
		// meaning, and only the count separates them.
		fmt.Printf("  NOT A PASS — no .mjs/.js artifact of %d, so no schema was composed at all\n", len(arts))
		return false, nil
	}
	drops := 0
	for _, a := range scripts {
		now, nowErr := censusOf(filepath.Join(w.Root, a))
		if errors.Is(nowErr, os.ErrNotExist) {
			// GONE since the reference. Not a drop and not a pass: what the run
			// briefs changed, and only the artifact list says how.
			fmt.Printf("  %s: GONE — the artifact is not in the working tree; it was compared at %s and is not now\n", a, ref)
			continue
		}
		old, oldErr := censusAt(w, ref, a)
		switch {
		case nowErr != nil:
			fmt.Printf("  %s: NOT A PASS — the working tree's copy did not run (%v)\n", a, nowErr)
			continue
		case oldErr != nil:
			// A script ADDED since the reference has no prior census, and that
			// is the first-review case `wf stats` names rather than counts: it
			// is not a drop and it is not a pass either.
			fmt.Printf("  %s: NOT A PASS — no comparable copy at %s (%v), so nothing was compared\n", a, ref, oldErr)
			continue
		}
		labels := map[string]bool{}
		for l := range now {
			labels[l] = true
		}
		for l := range old {
			labels[l] = true
		}
		names := make([]string, 0, len(labels))
		for l := range labels {
			names = append(names, l)
		}
		sort.Strings(names)
		changed := 0
		for _, l := range names {
			o, hadOld := old[l]
			n, hasNow := now[l]
			switch {
			case !hadOld:
				fmt.Printf("  %s %s: NEW — %d required key(s), %d byte(s)\n", a, l, n.Required, n.Bytes)
				changed++
			case !hasNow:
				fmt.Printf("  %s %s: GONE — it required %d key(s) at %s\n", a, l, o.Required, ref)
				changed++
			case o.Required < 0 || n.Required < 0:
				// One side did not SERIALIZE, so its census was never taken.
				// Comparing an unmeasured -1 against a real count would report
				// the largest DROP in the file over a schema nothing measured —
				// an absent measurement wearing a finding's clothes.
				changed++
				fmt.Printf("  %s %s: NOT A PASS — no census on the %s side (the schema does not serialize, a `$ref` did not expand, or this label composes more than one schema), so nothing was compared\n",
					a, l, map[bool]string{true: "working-tree", false: ref}[n.Required < 0])
			case n.Required < o.Required:
				drops++
				changed++
				fmt.Printf("  %s %s: DROP — %d required key(s), was %d (%s); %d byte(s), was %d\n",
					a, l, n.Required, o.Required, widthMove(o.Bytes, n.Bytes), n.Bytes, o.Bytes)
			case n.Required > o.Required:
				changed++
				fmt.Printf("  %s %s: rose — %d required key(s), was %d; %d byte(s), was %d\n",
					a, l, n.Required, o.Required, n.Bytes, o.Bytes)
			case n.NoRequired > o.NoRequired:
				drops++
				changed++
				fmt.Printf("  %s %s: DROP — %d object(s) declare properties and require none, was %d; %d byte(s), was %d\n",
					a, l, n.NoRequired, o.NoRequired, n.Bytes, o.Bytes)
			}
		}
		if changed == 0 {
			fmt.Printf("  %s: %d label(s), no change in what any schema requires\n", a, len(names))
		}
	}
	if drops > 0 {
		fmt.Printf("  %d label(s) require LESS than at %s. A schema that stops requiring a field is an agent allowed to omit it, and every reader downstream of that field reads nothing instead\n", drops, ref)
		return true, nil
	}
	return false, nil
}

// widthMove says whether the narrowing and the drop happened together, which is
// the shape both instances in this corpus took.
func widthMove(was, now int) string {
	switch {
	case now < was:
		return "inside a NARROWING edit"
	case now > was:
		return "while the schema grew"
	default:
		return "at an unchanged width"
	}
}

// censusOf runs the clean mode over one script and returns its per-label
// schema census. The clean mode alone, deliberately: the corpus's width is
// measured on the happy path and the two numbers must be the same run's.
func censusOf(path string) (map[string]wfdry.SchemaUse, error) {
	rs, err := wfdry.Run(path, []string{"clean"}, wfdry.Options{})
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return nil, fmt.Errorf("no result")
	}
	if rs[0].Err != nil {
		return nil, rs[0].Err
	}
	out := map[string]wfdry.SchemaUse{}
	for _, s := range rs[0].Schemas {
		prev, seen := out[s.Label]
		switch {
		case !seen:
			out[s.Label] = s
		case prev.Required != s.Required || prev.NoRequired != s.NoRequired || prev.Bytes != s.Bytes:
			// One label, two DIFFERENT schemas — a per-item briefing built in a
			// loop. Keeping the first would compare whichever ordinal happened
			// to run first against whichever ran first at the reference, and a
			// requirement lost on the second spawn would read as unchanged.
			// It is the ambiguity `check cite` reports and `--prompt-diff`
			// refuses, in a third place: UNMEASURED, by name.
			out[s.Label] = wfdry.SchemaUse{Label: s.Label, Bytes: -1, Required: -1, NoRequired: -1}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the clean mode composed no schema")
	}
	return out, nil
}

// censusAt materialises the artifact as of a reference and runs the same census
// over it. The copy is written outside the workspace: a stray file under
// `artifacts/` is what `wf verify` refuses, and a check must not create the
// state the next command objects to.
func censusAt(w *ws.Workspace, ref, art string) (map[string]wfdry.SchemaUse, error) {
	b, err := w.Git.ShowFile(ref, art)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "wf-schema-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	p := filepath.Join(dir, filepath.Base(art))
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return nil, err
	}
	return censusOf(p)
}

// scriptSet keeps the workspace-relative artifacts this check can run.
func scriptSet(arts []string) []string {
	var out []string
	for _, a := range arts {
		// A path at the reference may be anything in the repository; only the
		// artifacts are this check's population.
		if !strings.HasPrefix(filepath.ToSlash(a), "artifacts/") {
			continue
		}
		if ext := strings.ToLower(filepath.Ext(a)); ext == ".mjs" || ext == ".js" {
			out = append(out, a)
		}
	}
	return out
}
