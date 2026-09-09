package pager

// searchState holds the incremental search.
type searchState struct {
	// typing is true while the prompt is open and the query is being edited.
	typing bool
	// query is the committed search, kept after the prompt closes so that n
	// and N keep working.
	query string
	// draft is what has been typed so far, before Enter.
	draft string
	// matches are the indices of matching lines in the current layout.
	matches []int
	// index is the position within matches, or -1 before the first jump.
	index int
}

// begin opens the prompt, starting from the previous query so that repeating a
// search only needs Enter.
func (s *searchState) begin() {
	s.typing = true
	s.draft = s.query
}

// cancel closes the prompt without changing the committed query.
func (s *searchState) cancel() {
	s.typing = false
	s.draft = ""
}

// commit accepts the draft as the query.
func (s *searchState) commit() {
	s.typing = false
	s.query = s.draft
	s.draft = ""
	s.matches = nil
	s.index = -1
}

// append adds a character to the draft.
func (s *searchState) append(r rune) {
	s.draft += string(r)
}

// backspace removes the last character, closing the prompt if it empties.
func (s *searchState) backspace() {
	if s.draft == "" {
		s.cancel()
		return
	}
	runes := []rune(s.draft)
	s.draft = string(runes[:len(runes)-1])
}

// step moves to the next match in the given direction and returns the line to
// scroll to.
//
// Before the first jump, index is -1 and the search starts from wherever the
// reader is rather than from the top of the document, which is what makes
// searching from halfway down behave as expected.
func (s *searchState) step(direction, from int) (line int, ok bool) {
	if len(s.matches) == 0 {
		return 0, false
	}

	if s.index < 0 {
		if direction > 0 {
			// The first match at or after the current position.
			for i, m := range s.matches {
				if m >= from {
					s.index = i
					return m, true
				}
			}
			s.index = 0 // wrapped around
			return s.matches[0], true
		}
		for i := len(s.matches) - 1; i >= 0; i-- {
			if s.matches[i] < from {
				s.index = i
				return s.matches[i], true
			}
		}
		s.index = len(s.matches) - 1
		return s.matches[s.index], true
	}

	// Wrapping is deliberate: reaching the end and continuing from the start
	// is what every search in a pager does.
	s.index = (s.index + direction + len(s.matches)) % len(s.matches)
	return s.matches[s.index], true
}

// active reports whether matches should be highlighted.
func (s *searchState) active() bool {
	return s.query != "" || (s.typing && s.draft != "")
}

// highlightQuery is the text to highlight, which follows the draft while the
// prompt is open so matches appear as the reader types.
func (s *searchState) highlightQuery() string {
	if s.typing {
		return s.draft
	}
	return s.query
}
