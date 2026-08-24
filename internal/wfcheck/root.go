package wfcheck

import (
	"errors"
	"os"
	"path/filepath"

	"git.sumka.site/yakov/wfgate/internal/ws"
)

// resolveRoot answers which repository the file-existence checks read when the
// caller did not name one.
//
// Measured on mocker slice A: `wfcheck` run from the gate workspace without
// `-root` reported twenty-two "owns X, which does not exist" WARNs on a correct
// script, because a script's ownership paths are relative to the repository
// under review and the default was the working directory. The workspace already
// records that repository in gate.json, and twenty-two warnings on correct work
// is exactly the noise that trains an author to skim the output.
//
// It returns the root and the note explaining where it came from — a resolution
// the reader did not ask for must say so, or the next divergence between the two
// trees reads as a script defect.
func resolveRoot(root string) (string, string) {
	if root != "" {
		return root, ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ".", ""
	}
	w, err := ws.Find(cwd)
	// No workspace above here is the ordinary case and says nothing. A
	// workspace that is THERE and could not be read is a different fact, and
	// falling back on it silently is how a check answers about the wrong tree
	// and exits zero: the reader asked for no root precisely because they
	// expected the workspace to supply one.
	if err != nil {
		if errors.Is(err, ws.ErrNotFound) {
			return cwd, ""
		}
		return cwd, "a gate workspace is here but could not be read (" + err.Error() + ") — the file-existence checks below read " + cwd + ", which is the workspace and not the repository under review"
	}
	if w.M.Target == "" {
		return cwd, "gate workspace " + filepath.Base(w.Root) + " names no target — the file-existence checks below read " + cwd + " instead"
	}
	if st, err := os.Stat(w.M.Target); err != nil || !st.IsDir() {
		// A workspace naming a target that is not there is worth saying out
		// loud: the ownership checks are then answering about the wrong tree.
		return cwd, "gate workspace " + filepath.Base(w.Root) + " names target " + w.M.Target + ", which is not a directory — the file-existence checks below read " + cwd + " instead"
	}
	return w.M.Target, "-root not given: reading " + w.M.Target + ", the target of gate workspace " + filepath.Base(w.Root) + " (a script's owned paths are relative to the repository under review, never to the workspace)"
}
