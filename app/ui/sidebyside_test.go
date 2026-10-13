package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/revdiff/app/annotation"
	"github.com/umputun/revdiff/app/diff"
)

func sbsTestModel(t *testing.T) Model {
	t.Helper()
	lines := []diff.DiffLine{
		{OldNum: 1, NewNum: 1, Content: "ctx1", ChangeType: diff.ChangeContext},
		{OldNum: 2, Content: "old2", ChangeType: diff.ChangeRemove},
		{OldNum: 3, Content: "old3", ChangeType: diff.ChangeRemove},
		{NewNum: 2, Content: "new2", ChangeType: diff.ChangeAdd},
		{OldNum: 4, NewNum: 3, Content: "ctx4", ChangeType: diff.ChangeContext},
		{NewNum: 4, Content: "new4", ChangeType: diff.ChangeAdd},
	}
	m := testModel([]string{"a.go"}, map[string][]diff.DiffLine{"a.go": lines})
	m.layout.focus = paneDiff
	m.file.name = "a.go"
	m.file.lines = lines
	m.file.lineNumWidth = 1
	m.modes.sideBySide = true
	m.store.Add(annotation.Annotation{File: "a.go", Line: 2, Type: "-", Comment: "on old2"})
	m.store.Add(annotation.Annotation{File: "a.go", Line: 2, Type: "+", Comment: "on new2"})
	require.True(t, m.sideBySideActive())
	return m
}

func TestSideBySide_RowPairing(t *testing.T) {
	m := sbsTestModel(t)
	assert.Equal(t, []sbsRow{{0, 0}, {1, 3}, {2, -1}, {4, 4}, {-1, 5}}, m.sbsRows())
}

func TestSideBySide_RowMapping(t *testing.T) {
	m := sbsTestModel(t)
	// row 1 pairs old2|new2 and carries both annotations below it, left first
	tests := []struct {
		row   int
		idx   int
		onAnn bool
	}{
		{0, 0, false}, {1, 1, false}, {2, 1, true}, {3, 3, true},
		{4, 2, false}, {5, 4, false}, {6, 5, false}, {99, 5, false},
	}
	for _, tt := range tests {
		idx, onAnn := m.visualRowToDiffLine(tt.row)
		assert.Equal(t, tt.idx, idx, "row %d", tt.row)
		assert.Equal(t, tt.onAnn, onAnn, "row %d", tt.row)

		m.nav.diffCursor, m.annot.cursorOnAnnotation = tt.idx, tt.onAnn
		if tt.row < 99 {
			assert.Equal(t, tt.row, m.cursorViewportY(), "cursor y for line %d", tt.idx)
		}
	}

	m.nav.diffCursor, m.annot.cursorOnAnnotation = 3, false
	top, bottom := m.cursorVisualRange()
	assert.Equal(t, [2]int{1, 3}, [2]int{top, bottom}, "paired row spans its annotations")
}

func TestSideBySide_Render(t *testing.T) {
	m := sbsTestModel(t)
	m.nav.diffCursor = 3
	rows := strings.Split(strings.TrimSuffix(m.renderDiff(), "\n"), "\n")
	require.Len(t, rows, 7)
	assert.Contains(t, rows[1], "old2")
	assert.Contains(t, rows[1], "new2")
	assert.Less(t, strings.Index(rows[1], "old2"), strings.Index(rows[1], "new2"))
	assert.Contains(t, rows[2], "on old2")
	assert.Contains(t, rows[3], "on new2")
	for _, i := range []int{0, 1, 4, 5, 6} {
		assert.Equal(t, m.diffContentWidth()+1, lipgloss.Width(rows[i]), "row %d width", i)
	}
	before, _, found := strings.Cut(rows[6], "new4")
	require.True(t, found)
	assert.Greater(t, lipgloss.Width(before), m.diffContentWidth()/2, "add-only row sits on the right")
}
