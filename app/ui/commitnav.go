package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// shortHashLen is how much of a commit hash the UI shows. Matches the width git
// itself abbreviates to by default.
const shortHashLen = 7

// maxAnchorLen caps the anchor text kept with an annotation. Long enough to
// identify a line of code, short enough that a minified or generated line does
// not dominate the output.
const maxAnchorLen = 200

// handleCommitNav moves the review one commit forward or back. The ref swap is
// the whole mechanism: the new stop's ref replaces the session ref and the
// standard reload re-reads the tree and the diff through it.
//
// Annotations are deliberately kept. They belong to the commit they were written
// against — the store keeps them under that commit and only changes which ones
// the current scope sees — so walking back and forth never loses a comment.
func (m Model) handleCommitNav(forward bool) (tea.Model, tea.Cmd) {
	if !m.commitNav.enabled() {
		m.commitNav.hint = "commit navigation needs --per-commit"
		return m, nil
	}

	next := m.commitNav.idx + 1
	if !forward {
		next = m.commitNav.idx - 1
	}
	if next < 0 {
		m.commitNav.hint = "already at the first commit"
		return m, nil
	}
	if next >= len(m.commitNav.stops) {
		m.commitNav.hint = "already at the last commit"
		return m, nil
	}

	m.commitNav.idx = next
	stop := m.commitNav.current()
	m.cfg.ref = stop.Ref
	m.store.SetCommit(stop.Hash)
	m.commitNav.hint = fmt.Sprintf("commit %d/%d %s %s", next+1, len(m.commitNav.stops), shortHash(stop.Hash), stop.Subject)
	cmd := m.triggerReload()
	return m, cmd
}

// commitSegment returns the status-bar position indicator for a per-commit
// review, or empty outside one.
func (m Model) commitSegment() string {
	if !m.commitNav.enabled() {
		return ""
	}
	return fmt.Sprintf("commit %d/%d %s", m.commitNav.idx+1, len(m.commitNav.stops), shortHash(m.commitNav.current().Hash))
}

// shortHash abbreviates a commit hash for display.
func shortHash(hash string) string {
	if len(hash) <= shortHashLen {
		return hash
	}
	return hash[:shortHashLen]
}
