package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/diff"
)

func TestValidatePerCommitFlag(t *testing.T) {
	base := func() options {
		var o options
		o.PerCommit = true
		o.Refs.Base = "main"
		return o
	}

	t.Run("ref range is enough", func(t *testing.T) {
		require.NoError(t, validatePerCommitFlag(base()))
	})

	t.Run("off is always fine", func(t *testing.T) {
		var o options
		require.NoError(t, validatePerCommitFlag(o))
	})

	t.Run("needs a ref", func(t *testing.T) {
		o := base()
		o.Refs.Base = ""
		require.ErrorContains(t, validatePerCommitFlag(o), "requires a ref")
	})

	t.Run("rejects staged", func(t *testing.T) {
		o := base()
		o.Staged = true
		require.ErrorContains(t, validatePerCommitFlag(o), "--staged")
	})

	t.Run("rejects all-files", func(t *testing.T) {
		o := base()
		o.AllFiles = true
		require.ErrorContains(t, validatePerCommitFlag(o), "--all-files")
	})

	t.Run("rejects stdin", func(t *testing.T) {
		o := base()
		o.Stdin = true
		require.ErrorContains(t, validatePerCommitFlag(o), "--stdin")
	})

	t.Run("rejects compare mode", func(t *testing.T) {
		o := base()
		o.CompareNew = "new.txt"
		require.ErrorContains(t, validatePerCommitFlag(o), "compare mode")
	})
}

func TestParseArgs_PerCommit(t *testing.T) {
	opts, err := parseArgs([]string{"--per-commit", "main..HEAD"})
	require.NoError(t, err)
	assert.True(t, opts.PerCommit)

	_, err = parseArgs([]string{"--per-commit"})
	require.ErrorContains(t, err, "requires a ref")
}

// perCommitRepo builds a repo with three commits on top of a base and returns
// its path plus the hashes, oldest first.
func perCommitRepo(t *testing.T) (dir string, hashes []string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...) //nolint:gosec // test-controlled args
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	write := func(content string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o600))
	}
	write("base\n")
	run("add", "f.txt")
	run("commit", "-m", "base")

	for _, subject := range []string{"first", "second", "third"} {
		write("base\n" + subject + "\n")
		run("commit", "-am", subject)
		out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output() //nolint:gosec // test-controlled args
		require.NoError(t, err)
		hashes = append(hashes, strings.TrimSpace(string(out)))
	}
	return dir, hashes
}

func TestResolveCommitStops(t *testing.T) {
	dir, hashes := perCommitRepo(t)
	g := diff.NewGit(dir)

	stops, err := resolveCommitStops(g, "main~3..main", &bytes.Buffer{})
	require.NoError(t, err)
	require.Len(t, stops, 3)

	t.Run("oldest first", func(t *testing.T) {
		assert.Equal(t, hashes[0], stops[0].Hash)
		assert.Equal(t, hashes[2], stops[2].Hash)
		assert.Equal(t, []string{"first", "second", "third"}, []string{stops[0].Subject, stops[1].Subject, stops[2].Subject})
	})

	t.Run("each ref covers only its commit", func(t *testing.T) {
		for i, stop := range stops {
			lines, err := g.FileDiff(diff.FileDiffRequest{Ref: stop.Ref, Path: "f.txt"})
			require.NoError(t, err, "stop %d", i)
			var added []string
			for _, l := range lines {
				if l.ChangeType == diff.ChangeAdd {
					added = append(added, l.Content)
				}
			}
			assert.Equal(t, []string{stop.Subject}, added, "stop %d diffs only its own change", i)
		}
	})
}

func TestResolveCommitStops_Errors(t *testing.T) {
	dir, _ := perCommitRepo(t)
	g := diff.NewGit(dir)

	t.Run("nil logger", func(t *testing.T) {
		_, err := resolveCommitStops(nil, "main", &bytes.Buffer{})
		require.ErrorContains(t, err, "commit log")
	})

	t.Run("empty range", func(t *testing.T) {
		_, err := resolveCommitStops(g, "main..main", &bytes.Buffer{})
		require.ErrorContains(t, err, "no commits")
	})

	t.Run("logger without scoping", func(t *testing.T) {
		_, err := resolveCommitStops(commitLoggerOnly{}, "main", &bytes.Buffer{})
		require.ErrorContains(t, err, "not supported")
	})
}

// commitLoggerOnly implements diff.CommitLogger without diff.CommitScoper.
type commitLoggerOnly struct{}

func (commitLoggerOnly) CommitLog(string) ([]diff.CommitInfo, error) {
	return []diff.CommitInfo{{Hash: "abc"}}, nil
}
