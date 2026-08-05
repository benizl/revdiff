package annotation

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/umputun/revdiff/app/fsutil"
)

// Annotation represents a user comment on a specific diff line.
//
// Commit and Anchor are populated only in per-commit review mode. Line numbers
// are in the coordinates of Commit's own diff, not of the branch tip, so a
// consumer needs Commit to resolve them; Anchor carries the verbatim text of the
// annotated line so the position can also be recovered by content once the line
// has moved.
type Annotation struct {
	File    string // file path relative to repo root
	Line    int    // line number in the diff
	EndLine int    // end line of hunk range, 0 means no range
	Type    string // change type: "+", "-", or " "
	Comment string // user comment text
	Commit  string // commit the line numbers belong to; empty outside per-commit mode
	Anchor  string // verbatim text of the annotated line; empty when unavailable
}

// Store holds annotations in memory, keyed by filename.
//
// A Store carries a current commit, set by SetCommit and empty by default. The
// per-line operations (Add, Get, Has, Delete, Files) act only on annotations
// belonging to that commit, so callers working in terms of the diff on screen
// need no commit argument of their own. The whole-store operations (Count, All,
// Clear, FormatOutput, WriteFile) span every commit. With the commit left empty
// the two sets coincide.
type Store struct {
	annotations  map[string][]Annotation
	commit       string            // current scope; "" outside per-commit mode
	commitOrder  []string          // commit hashes in review order, for FormatOutput grouping
	commitLabels map[string]string // hash -> subject, for FormatOutput section headers
}

// NewStore creates a new empty annotation store.
func NewStore() *Store {
	return &Store{annotations: make(map[string][]Annotation), commitLabels: make(map[string]string)}
}

// SetCommit sets the commit the per-line operations act on. Empty restores
// unscoped behavior.
func (s *Store) SetCommit(hash string) {
	s.commit = hash
}

// Commit returns the current commit scope.
func (s *Store) Commit() string {
	return s.commit
}

// SetCommitOrder records the commits under review, oldest first, alongside their
// subjects. FormatOutput emits sections in this order; commits missing from it
// sort after the known ones, by hash.
func (s *Store) SetCommitOrder(hashes, subjects []string) {
	s.commitOrder = append([]string(nil), hashes...)
	s.commitLabels = make(map[string]string, len(hashes))
	for i, h := range hashes {
		if i < len(subjects) {
			s.commitLabels[h] = subjects[i]
		}
	}
}

// Add adds an annotation for the given file and line, stamping it with the
// current commit scope. If an annotation already exists at the same file:line
// within that scope, it is replaced.
func (s *Store) Add(a Annotation) {
	if a.Commit == "" {
		a.Commit = s.commit
	}
	existing := s.annotations[a.File]
	if i, ok := s.findIn(a.File, a.Line, a.Type, a.Commit); ok {
		existing[i].Comment = a.Comment
		existing[i].EndLine = a.EndLine
		existing[i].Anchor = a.Anchor
		return
	}
	s.annotations[a.File] = append(existing, a)
}

// Delete removes the annotation at the given file, line and change type.
// Returns true if an annotation was found and removed.
func (s *Store) Delete(file string, line int, changeType string) bool {
	i, ok := s.find(file, line, changeType)
	if !ok {
		return false
	}
	existing := s.annotations[file]
	s.annotations[file] = append(existing[:i], existing[i+1:]...)
	if len(s.annotations[file]) == 0 {
		delete(s.annotations, file)
	}
	return true
}

// Has checks if an annotation exists at the given file, line and change type.
func (s *Store) Has(file string, line int, changeType string) bool {
	_, ok := s.find(file, line, changeType)
	return ok
}

// find returns the index of an annotation in the current commit scope matching
// file, line, and changeType.
func (s *Store) find(file string, line int, changeType string) (int, bool) {
	return s.findIn(file, line, changeType, s.commit)
}

// findIn returns the index of an annotation matching file, line, changeType and
// commit.
func (s *Store) findIn(file string, line int, changeType, commit string) (int, bool) {
	for i, a := range s.annotations[file] {
		if a.Line == line && a.Type == changeType && a.Commit == commit {
			return i, true
		}
	}
	return 0, false
}

// Get returns the current commit's annotations for the given file, sorted by
// line number.
func (s *Store) Get(file string) []Annotation {
	result := make([]Annotation, 0, len(s.annotations[file]))
	for _, a := range s.annotations[file] {
		if a.Commit == s.commit {
			result = append(result, a)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Line < result[j].Line })
	return result
}

// Count returns the total number of annotations across all files.
func (s *Store) Count() int {
	count := 0
	for _, anns := range s.annotations {
		count += len(anns)
	}
	return count
}

// Clear removes all annotations from the store.
func (s *Store) Clear() {
	s.annotations = make(map[string][]Annotation)
}

// All returns all annotations grouped by file. The returned map is a copy.
func (s *Store) All() map[string][]Annotation {
	result := make(map[string][]Annotation, len(s.annotations))
	for file, anns := range s.annotations {
		copied := make([]Annotation, len(anns))
		copy(copied, anns)
		sort.Slice(copied, func(i, j int) bool { return copied[i].Line < copied[j].Line })
		result[file] = copied
	}
	return result
}

// Files returns the files that have annotations in the current commit scope,
// sorted alphabetically.
func (s *Store) Files() []string {
	files := make([]string, 0, len(s.annotations))
	for file, anns := range s.annotations {
		for _, a := range anns {
			if a.Commit == s.commit {
				files = append(files, file)
				break
			}
		}
	}
	sort.Strings(files)
	return files
}

// allFiles returns every file with annotations regardless of commit, sorted
// alphabetically.
func (s *Store) allFiles() []string {
	files := make([]string, 0, len(s.annotations))
	for file := range s.annotations {
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

// Load parses markdown produced by FormatOutput from r and adds each recovered
// annotation via Add (so duplicate file/line/type pairs apply last-write-wins).
// It is the symmetric inverse of FormatOutput on the API surface; callers that
// need to filter records (e.g. drop orphans against a diff) should use Parse
// directly and Add the survivors themselves.
func (s *Store) Load(r io.Reader) error {
	records, err := Parse(r)
	if err != nil {
		return err
	}
	for _, a := range records {
		s.Add(a)
	}
	return nil
}

// FormatOutput produces the structured output format for stdout.
// Files are sorted alphabetically, annotations within each file by line number.
// Returns empty string if no annotations exist.
//
// Annotations carrying a commit are grouped into "# commit <hash> <subject>"
// sections, in the order given to SetCommitOrder. The single-hash heading level
// keeps sections clear of the "## " record-header grammar, so a reader that only
// knows about records sees them as prose. Records with no commit are emitted
// first, ungrouped, which is the whole output outside per-commit mode.
//
// A record header ends with " | <verbatim line>" when the annotation has an
// anchor, giving a consumer the annotated line's content next to its number.
//
// Body lines that start with "## " (the record-header form) are prefixed with a
// single space on output so parsers that split on "## " record headers cannot
// mistake a comment line for a new record. The added space is cosmetic
// (markdown renderers treat leading whitespace before a heading marker as
// paragraph text) and preserves the original text when whitespace is trimmed
// per line. Other markdown heading forms like "### subheader" are not escaped.
func (s *Store) FormatOutput() string {
	if len(s.annotations) == 0 {
		return ""
	}

	var buf strings.Builder
	first := true
	for _, commit := range s.outputCommits() {
		if commit != "" {
			if !first {
				buf.WriteString("\n")
			}
			fmt.Fprintf(&buf, "# commit %s", commit)
			if subject := s.commitLabels[commit]; subject != "" {
				fmt.Fprintf(&buf, " %s", subject)
			}
			buf.WriteString("\n")
			first = true // the section's first record needs no separator of its own
		}
		for _, file := range s.allFiles() {
			for _, a := range s.commitAnnotations(file, commit) {
				if !first {
					buf.WriteString("\n")
				}
				first = false
				buf.WriteString(s.formatRecord(a))
			}
		}
	}
	return buf.String()
}

// formatRecord renders one annotation as a header line plus body.
func (s *Store) formatRecord(a Annotation) string {
	var head string
	switch {
	case a.Line == 0:
		head = fmt.Sprintf("## %s (file-level)", a.File)
	case a.EndLine > 0:
		head = fmt.Sprintf("## %s:%d-%d (%s)", a.File, a.Line, a.EndLine, a.Type)
	default:
		head = fmt.Sprintf("## %s:%d (%s)", a.File, a.Line, a.Type)
	}
	if a.Anchor != "" {
		head += " | " + a.Anchor
	}
	return head + "\n" + s.escapeHeaderLines(a.Comment) + "\n"
}

// commitAnnotations returns the file's annotations for one commit, sorted by
// line number (file-level first, then ascending).
func (s *Store) commitAnnotations(file, commit string) []Annotation {
	result := make([]Annotation, 0, len(s.annotations[file]))
	for _, a := range s.annotations[file] {
		if a.Commit == commit {
			result = append(result, a)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Line < result[j].Line })
	return result
}

// outputCommits returns the commits present in the store, ordered by
// commitOrder. The empty commit sorts first; commits absent from commitOrder
// follow the known ones, sorted by hash.
func (s *Store) outputCommits() []string {
	present := make(map[string]bool)
	for _, anns := range s.annotations {
		for _, a := range anns {
			present[a.Commit] = true
		}
	}
	ordered := make([]string, 0, len(present))
	if present[""] {
		ordered = append(ordered, "")
		delete(present, "")
	}
	for _, h := range s.commitOrder {
		if present[h] {
			ordered = append(ordered, h)
			delete(present, h)
		}
	}
	rest := make([]string, 0, len(present))
	for h := range present {
		rest = append(rest, h)
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

// WriteFile writes FormatOutput to path atomically (temp file + rename, mode
// 0o600) and returns the exact snapshot written, so a concurrent reader sees
// either the old or the new complete file, never a truncated one.
func (s *Store) WriteFile(path string) (string, error) {
	content := s.FormatOutput()
	if err := fsutil.AtomicWriteFile(path, []byte(content)); err != nil {
		return "", fmt.Errorf("write annotations to %s: %w", path, err)
	}
	return content, nil
}

// escapeHeaderLines prefixes any body line whose first non-space content is
// "## ", or which reads as a "# commit <hash>" section header, with a single
// extra space. The parser inverts this by stripping one leading space from such
// body lines. Escaping pre-indented variants (e.g. " ## ") keeps the round-trip
// symmetric for arbitrary user content. Other heading forms like "### " are
// not escaped since they cannot collide with either split marker.
func (s *Store) escapeHeaderLines(body string) string {
	if !strings.Contains(body, "## ") && !strings.Contains(body, "# commit ") {
		return body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "## ") || commitSectionRe.MatchString(trimmed) {
			lines[i] = " " + line
		}
	}
	return strings.Join(lines, "\n")
}
