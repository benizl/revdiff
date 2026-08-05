package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/annotation"
	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/keymap"
	"github.com/umputun/revdiff/app/ui/mocks"
	"github.com/umputun/revdiff/app/ui/overlay"
	"github.com/umputun/revdiff/app/ui/style"
	"github.com/umputun/revdiff/app/ui/worddiff"
)

func testStops() []CommitStop {
	return []CommitStop{
		{Hash: "aaa1111111111111111111111111111111111111", Subject: "add parser", Ref: "aaa1111111111111111111111111111111111111^..aaa1111111111111111111111111111111111111"},
		{Hash: "bbb2222222222222222222222222222222222222", Subject: "fix parser", Ref: "bbb2222222222222222222222222222222222222^..bbb2222222222222222222222222222222222222"},
		{Hash: "ccc3333333333333333333333333333333333333", Subject: "test parser", Ref: "ccc3333333333333333333333333333333333333^..ccc3333333333333333333333333333333333333"},
	}
}

// perCommitModel builds a per-commit model over the given stops with one file in
// the tree whose diff has a single added line.
func perCommitModel(t *testing.T, store *annotation.Store, stops []CommitStop) Model {
	t.Helper()
	renderer := &mocks.RendererMock{
		ChangedFilesFunc: func(ref string, staged bool) ([]diff.FileEntry, error) {
			return []diff.FileEntry{{Path: "a.go"}}, nil
		},
		FileDiffFunc: func(req diff.FileDiffRequest) ([]diff.DiffLine, error) {
			return []diff.DiffLine{{ChangeType: diff.ChangeAdd, NewNum: 12, Content: "\tf, _ := os.Open(p)"}}, nil
		},
	}
	res := style.PlainResolver()
	m, err := NewModel(ModelConfig{
		Renderer:         renderer,
		Store:            store,
		Highlighter:      noopHighlighter(),
		StyleResolver:    res,
		StyleRenderer:    style.NewRenderer(res),
		SGR:              style.SGR{},
		WordDiffer:       worddiff.New(),
		Overlay:          overlay.NewManager(),
		Themes:           fakeThemeCatalog{},
		TreeWidthRatio:   3,
		AnnotationMarker: "\U0001f4ac",
		NewFileTree:      testFileTreeFactory(),
		ParseTOC:         testParseTOCFactory(),
		Ref:              "main..HEAD",
		CommitStops:      stops,
		ReloadApplicable: true,
	})
	require.NoError(t, err)
	m.layout.width = 120
	m.layout.height = 40
	m.ready = true
	m.filesLoaded = true
	return m
}

func TestNewModel_PerCommitStartsOnFirstStop(t *testing.T) {
	store := annotation.NewStore()
	stops := testStops()
	m := perCommitModel(t, store, stops)

	assert.True(t, m.commitNav.enabled())
	assert.Equal(t, stops[0].Ref, m.cfg.ref, "session ref is scoped to the first commit")
	assert.Equal(t, "main..HEAD", m.commitNav.baseRef, "whole range kept for the info popup")
	assert.Equal(t, stops[0].Hash, store.Commit())
}

func TestNewModel_WithoutStopsKeepsRangeRef(t *testing.T) {
	m := perCommitModel(t, annotation.NewStore(), nil)
	assert.False(t, m.commitNav.enabled())
	assert.Equal(t, "main..HEAD", m.cfg.ref)
	assert.Empty(t, m.commitSegment())
}

func TestHandleCommitNav_MovesScope(t *testing.T) {
	store := annotation.NewStore()
	stops := testStops()
	m := perCommitModel(t, store, stops)

	model, cmd := m.handleCommitNav(true)
	next := model.(Model)
	require.NotNil(t, cmd, "moving commits reloads the diff")
	assert.Equal(t, 1, next.commitNav.idx)
	assert.Equal(t, stops[1].Ref, next.cfg.ref)
	assert.Equal(t, stops[1].Hash, store.Commit())
	assert.Contains(t, next.commitNav.hint, "commit 2/3")
	assert.Contains(t, next.commitNav.hint, "fix parser")

	back, cmd := next.handleCommitNav(false)
	require.NotNil(t, cmd)
	assert.Equal(t, 0, back.(Model).commitNav.idx)
	assert.Equal(t, stops[0].Ref, back.(Model).cfg.ref)
}

func TestHandleCommitNav_StopsAtEnds(t *testing.T) {
	store := annotation.NewStore()
	m := perCommitModel(t, store, testStops())

	first, cmd := m.handleCommitNav(false)
	assert.Nil(t, cmd, "no reload at the first commit")
	assert.Equal(t, 0, first.(Model).commitNav.idx)
	assert.Contains(t, first.(Model).commitNav.hint, "first commit")

	m.commitNav.idx = 2
	last, cmd := m.handleCommitNav(true)
	assert.Nil(t, cmd, "no reload at the last commit")
	assert.Equal(t, 2, last.(Model).commitNav.idx)
	assert.Contains(t, last.(Model).commitNav.hint, "last commit")
}

func TestHandleCommitNav_WithoutPerCommitHints(t *testing.T) {
	m := perCommitModel(t, annotation.NewStore(), nil)
	model, cmd := m.handleCommitNav(true)
	assert.Nil(t, cmd)
	assert.Contains(t, model.(Model).commitNav.hint, "--per-commit")
}

func TestCommitNav_AnnotationsSurviveTheWalk(t *testing.T) {
	store := annotation.NewStore()
	stops := testStops()
	m := perCommitModel(t, store, stops)

	store.Add(annotation.Annotation{File: "a.go", Line: 12, Type: "+", Comment: "on first commit"})
	model, _ := m.handleCommitNav(true)
	m = model.(Model)
	store.Add(annotation.Annotation{File: "a.go", Line: 12, Type: "+", Comment: "on second commit"})

	assert.Equal(t, 2, store.Count(), "same file:line in two commits are distinct annotations")
	require.Len(t, store.Get("a.go"), 1)
	assert.Equal(t, "on second commit", store.Get("a.go")[0].Comment)

	back, _ := m.handleCommitNav(false)
	_ = back
	require.Len(t, store.Get("a.go"), 1)
	assert.Equal(t, "on first commit", store.Get("a.go")[0].Comment)
}

func TestCommitSegment(t *testing.T) {
	m := perCommitModel(t, annotation.NewStore(), testStops())
	assert.Equal(t, "commit 1/3 aaa1111", m.commitSegment())
	m.commitNav.idx = 2
	assert.Equal(t, "commit 3/3 ccc3333", m.commitSegment())
}

func TestTransientHint_CommitNav(t *testing.T) {
	m := perCommitModel(t, annotation.NewStore(), testStops())
	m.commitNav.hint = "commit 2/3 bbb2222 fix parser"
	assert.Equal(t, "commit 2/3 bbb2222 fix parser", m.transientHint())

	m.reload.hint = "Reloaded"
	assert.Equal(t, "Reloaded", m.transientHint(), "reload hint outranks commit hint")
}

func TestDispatchAction_CommitNavBound(t *testing.T) {
	store := annotation.NewStore()
	stops := testStops()
	m := perCommitModel(t, store, stops)

	model, _ := m.dispatchAction(keymap.ActionNextCommit)
	assert.Equal(t, 1, model.(Model).commitNav.idx)
}

func TestLineAnchor(t *testing.T) {
	store := annotation.NewStore()
	m := perCommitModel(t, store, testStops())
	m.file.name = "a.go"
	m.file.lines = []diff.DiffLine{{ChangeType: diff.ChangeAdd, NewNum: 12, Content: "\tf, _ := os.Open(p)"}}

	assert.Equal(t, "\tf, _ := os.Open(p)", m.lineAnchor("a.go", 12, "+"))
	assert.Empty(t, m.lineAnchor("a.go", 99, "+"), "line not in the diff has no anchor")
	assert.Empty(t, m.lineAnchor("other.go", 12, "+"), "anchor only from the loaded file")

	whole := perCommitModel(t, annotation.NewStore(), nil)
	whole.file.name = "a.go"
	whole.file.lines = m.file.lines
	assert.Empty(t, whole.lineAnchor("a.go", 12, "+"), "no anchor outside per-commit mode")
}

func TestLineAnchor_Truncates(t *testing.T) {
	long := strings.Repeat("x", maxAnchorLen+50)
	m := perCommitModel(t, annotation.NewStore(), testStops())
	m.file.name = "a.go"
	m.file.lines = []diff.DiffLine{{ChangeType: diff.ChangeAdd, NewNum: 1, Content: long}}

	got := m.lineAnchor("a.go", 1, "+")
	assert.Equal(t, strings.Repeat("x", maxAnchorLen)+"…", got)
}
