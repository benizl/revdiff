package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/umputun/revdiff/app/diff"
	"github.com/umputun/revdiff/app/ui/style"
)

// sbsRow is one rendered row of the side-by-side view, holding the line index shown on
// each side or -1 for a padded side. Context and divider lines span both sides (left == right).
type sbsRow struct{ left, right int }

// sbsLayout places side-by-side rows on visual rows. top has one entry per row plus a
// final entry holding the total height; rowOf maps each line index to its row.
type sbsLayout struct {
	rows  []sbsRow
	top   []int
	rowOf []int
}

// sideBySideActive reports whether the diff renders side by side. Collapsed mode and
// full-context files (no old/new distinction) keep the inline layout.
func (m Model) sideBySideActive() bool {
	return m.modes.sideBySide && !m.modes.collapsed.enabled && !m.file.singleColLineNum && len(m.file.lines) > 0
}

// toggleSideBySide switches between the inline and side-by-side diff layouts.
func (m *Model) toggleSideBySide() {
	if m.file.name == "" {
		return
	}
	m.modes.sideBySide = !m.modes.sideBySide
	m.syncViewportToCursor()
}

// sbsRows pairs each run of removed lines with the run of added lines that follows it.
func (m Model) sbsRows() []sbsRow {
	lines := m.file.lines
	rows := make([]sbsRow, 0, len(lines))
	for i := 0; i < len(lines); {
		if ct := lines[i].ChangeType; ct != diff.ChangeRemove && ct != diff.ChangeAdd {
			rows = append(rows, sbsRow{left: i, right: i})
			i++
			continue
		}
		rs := i
		for i < len(lines) && lines[i].ChangeType == diff.ChangeRemove {
			i++
		}
		as := i
		for i < len(lines) && lines[i].ChangeType == diff.ChangeAdd {
			i++
		}
		nr, na := as-rs, i-as
		for k := range max(nr, na) {
			r := sbsRow{left: -1, right: -1}
			if k < nr {
				r.left = rs + k
			}
			if k < na {
				r.right = as + k
			}
			rows = append(rows, r)
		}
	}
	return rows
}

// sbsAnnotRows returns the annotation rows rendered below line idx, 0 when it has none.
func (m Model) sbsAnnotRows(idx int, annSet map[string]bool) int {
	if idx < 0 || m.file.lines[idx].ChangeType == diff.ChangeDivider {
		return 0
	}
	dl := m.file.lines[idx]
	key := m.annotationKey(m.diffLineNum(dl), string(dl.ChangeType))
	if !annSet[key] {
		return 0
	}
	return m.wrappedAnnotationLineCount(key)
}

// sbsLayout lays rows out top to bottom: each row is one diff row followed by the left
// side's annotation, then the right side's.
func (m Model) sbsLayout(annSet map[string]bool) sbsLayout {
	l := sbsLayout{rows: m.sbsRows(), rowOf: make([]int, len(m.file.lines))}
	l.top = make([]int, len(l.rows)+1)
	y := 0
	if m.hasFileAnnotation() {
		y = m.wrappedAnnotationLineCount(annotKeyFile)
	}
	for r, row := range l.rows {
		l.top[r] = y
		y += 1 + m.sbsAnnotRows(row.left, annSet)
		if row.right != row.left {
			y += m.sbsAnnotRows(row.right, annSet)
		}
		if row.left >= 0 {
			l.rowOf[row.left] = r
		}
		if row.right >= 0 {
			l.rowOf[row.right] = r
		}
	}
	l.top[len(l.rows)] = y
	return l
}

// lineY returns the visual row of line idx, or of its annotation when onAnnotation is set.
func (m Model) sbsLineY(l sbsLayout, idx int, onAnnotation bool, annSet map[string]bool) int {
	r := l.rowOf[idx]
	y := l.top[r]
	if !onAnnotation {
		return y
	}
	y++
	if row := l.rows[r]; idx == row.right && row.left != row.right {
		y += m.sbsAnnotRows(row.left, annSet)
	}
	return y
}

// sbsVisualRowToDiffLine is visualRowToDiffLine for the side-by-side layout. A diff row
// resolves to its left line when present.
func (m Model) sbsVisualRowToDiffLine(row int) (idx int, onAnnotation bool) {
	annSet := m.buildAnnotationSet()
	l := m.sbsLayout(annSet)
	if row < l.top[0] {
		if m.hasFileAnnotation() {
			return -1, false
		}
		row = l.top[0]
	}
	r := sort.Search(len(l.rows), func(i int) bool { return l.top[i+1] > row })
	if r == len(l.rows) {
		r--
		row = l.top[r]
	}
	sr := l.rows[r]
	rel := row - l.top[r]
	if rel == 0 {
		if sr.left >= 0 {
			return sr.left, false
		}
		return sr.right, false
	}
	if sr.left >= 0 && rel-1 < m.sbsAnnotRows(sr.left, annSet) {
		return sr.left, true
	}
	return sr.right, true
}

// renderSideBySide writes every row with the annotations of its lines below it.
func (m Model) renderSideBySide(b *strings.Builder, annotationMap map[annotLineKey]string) {
	total := m.diffContentWidth() + 1 // the inline layout's cursor column plus content
	leftW := total / 2
	for _, row := range m.sbsRows() {
		if row.left == row.right && m.file.lines[row.left].ChangeType == diff.ChangeDivider {
			m.renderDiffLine(b, row.left, m.file.lines[row.left])
			continue
		}
		b.WriteString(m.sbsHalf(row.left, leftW, true) + m.sbsSeparator() + m.sbsHalf(row.right, total-leftW-1, false) + "\n")
		if row.left >= 0 {
			m.renderAnnotationOrInput(b, row.left, annotationMap)
		}
		if row.right >= 0 && row.right != row.left {
			m.renderAnnotationOrInput(b, row.right, annotationMap)
		}
	}
}

// sbsHalf renders one side of a row as a cursor cell, an optional line number and the
// content, cut and padded to width. A missing line (idx < 0) renders as filler.
func (m Model) sbsHalf(idx, width int, old bool) string {
	paneBg := m.resolver.Color(style.ColorKeyDiffPaneBg)
	if idx < 0 {
		bg := m.resolver.FillerBg()
		if bg == "" {
			bg = paneBg
		}
		return " " + sbsPad("", width-1, bg)
	}
	dl := m.file.lines[idx]
	cursor := " "
	if m.isCursorLine(idx) {
		cursor = m.renderer.DiffCursor(m.cfg.noColors)
	}
	gutter := ""
	if m.modes.lineNumbers {
		n := dl.NewNum
		if old {
			n = dl.OldNum
		}
		gutter = m.resolver.Style(style.StyleKeyLineNumber).Render(fmt.Sprintf(" %*d", m.file.lineNumWidth, n))
	}
	_, text, hasHighlight := m.prepareLineContent(idx, dl)
	text = m.applyIntraLineHighlight(idx, dl.ChangeType, text)
	content := m.styleDiffContent(dl.ChangeType, m.linePrefix(dl.ChangeType), text, hasHighlight, m.search.matchSet[idx])
	cw := max(width-1-lipgloss.Width(gutter), 0)
	content = ansi.Cut(content, m.layout.scrollX, m.layout.scrollX+cw)
	bg := m.resolver.LineBg(dl.ChangeType)
	if bg == "" {
		bg = paneBg
	}
	return cursor + gutter + sbsPad(content, cw, bg)
}

// sbsSeparator returns the muted rule drawn between the two sides.
func (m Model) sbsSeparator() string {
	if fg := m.resolver.Color(style.ColorKeyMutedFg); fg != "" {
		return string(m.resolver.Color(style.ColorKeyDiffPaneBg)) + string(fg) + "│" + string(style.ResetFg) + string(style.ResetBg)
	}
	return "│"
}

// sbsPad pads s to width on bg. Unlike extendLineBg it pads even without a bg so the
// right side stays aligned.
func sbsPad(s string, width int, bg style.Color) string {
	pad := width - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	if bg == "" {
		return s + strings.Repeat(" ", pad)
	}
	return s + string(bg) + strings.Repeat(" ", pad) + "\033[49m"
}
