package main

import (
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/umputun/revdiff/app/annotation"
	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui"
)

// validatePerCommitFlag rejects --per-commit combinations that have no commits to
// walk: the mode needs a ref range, and every other mode either has no VCS
// history behind it (stdin, compare) or deliberately ignores refs (staged,
// all-files).
func validatePerCommitFlag(opts options) error {
	if !opts.PerCommit {
		return nil
	}
	if opts.ref() == "" {
		return errors.New("--per-commit requires a ref or ref range")
	}
	if opts.Staged {
		return errors.New("--per-commit cannot be used with --staged")
	}
	if opts.AllFiles {
		return errors.New("--per-commit cannot be used with --all-files")
	}
	if opts.Stdin {
		return errors.New("--per-commit cannot be used with --stdin")
	}
	if opts.CompareOld != "" || opts.CompareNew != "" {
		return errors.New("--per-commit cannot be used with compare mode")
	}
	return nil
}

// preloadReq carries the inputs of the --annotations preload from the
// composition root to whichever loader the session mode calls for.
type preloadReq struct {
	opts               options
	store              *annotation.Store
	renderer           ui.Renderer
	stops              []ui.CommitStop
	untrackedFn        func() ([]string, error)
	untrackedRenamesFn func([]string) ([]diff.FileEntry, error)
	workDir            string
	warn               io.Writer
}

// loadPreloadedAnnotations feeds --annotations into the store, validating records
// against each commit's own diff in a per-commit session and against the whole
// range otherwise. No-op when --annotations was not given.
func loadPreloadedAnnotations(r preloadReq) error {
	if r.opts.Annotations == "" {
		return nil
	}
	if r.opts.PerCommit {
		return preloadAnnotationsPerCommit(r.opts.Annotations, r.store, r.renderer, r.stops,
			r.untrackedFn, r.untrackedRenamesFn, r.workDir, r.warn)
	}
	return preloadAnnotations(r.opts.Annotations, r.store, r.renderer, r.opts.ref(), r.opts.Staged,
		r.untrackedFn, r.untrackedRenamesFn, r.workDir, r.warn)
}

// resolveCommitStops enumerates the commits in ref and pairs each with the ref
// that narrows the diff to that commit alone. Stops come back oldest first, the
// order the changes were written in, which is the order a reviewer reads them.
//
// The whole list is resolved here, before the TUI starts, so navigating between
// commits is a local index move and cannot fail halfway through a review.
func resolveCommitStops(logger diff.CommitLogger, ref string, warn io.Writer) ([]ui.CommitStop, error) {
	if logger == nil {
		return nil, errors.New("--per-commit needs a VCS with a commit log")
	}
	scoper, ok := logger.(diff.CommitScoper)
	if !ok {
		return nil, errors.New("--per-commit is not supported by this VCS")
	}

	commits, err := logger.CommitLog(ref)
	if err != nil {
		return nil, fmt.Errorf("--per-commit: %w", err)
	}
	if len(commits) == 0 {
		return nil, fmt.Errorf("--per-commit: no commits in %s", ref)
	}
	if len(commits) >= diff.MaxCommits && warn != nil {
		_, _ = fmt.Fprintf(warn, "warning: --per-commit capped at %d commits; older commits in %s are not reachable\n", diff.MaxCommits, ref)
	}

	stops := make([]ui.CommitStop, 0, len(commits))
	for _, c := range slices.Backward(commits) { // CommitLog returns newest first
		scoped, serr := scoper.CommitScope(c.Hash)
		if serr != nil {
			return nil, fmt.Errorf("--per-commit: %w", serr)
		}
		stops = append(stops, ui.CommitStop{Hash: c.Hash, Subject: c.Subject, Ref: scoped})
	}
	return stops, nil
}
