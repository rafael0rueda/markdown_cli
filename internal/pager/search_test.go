package pager

import "testing"

func TestSearchStateEditing(t *testing.T) {
	var s searchState

	s.begin()
	if !s.typing {
		t.Fatal("begin should open the prompt")
	}
	for _, r := range "term" {
		s.append(r)
	}
	if s.draft != "term" {
		t.Errorf("draft = %q", s.draft)
	}

	s.backspace()
	if s.draft != "ter" {
		t.Errorf("after backspace draft = %q", s.draft)
	}

	s.commit()
	if s.typing || s.query != "ter" {
		t.Errorf("after commit typing=%v query=%q", s.typing, s.query)
	}
}

// TestSearchBeginKeepsPreviousQuery means repeating a search only needs
// slash and Enter.
func TestSearchBeginKeepsPreviousQuery(t *testing.T) {
	s := searchState{query: "earlier"}
	s.begin()
	if s.draft != "earlier" {
		t.Errorf("draft = %q, want the previous query", s.draft)
	}
}

// TestSearchBackspaceOnEmptyCancels is the natural way to back out of a prompt
// you have just opened.
func TestSearchBackspaceOnEmptyCancels(t *testing.T) {
	var s searchState
	s.begin()
	s.backspace()
	if s.typing {
		t.Error("backspacing an empty prompt should close it")
	}
}

func TestSearchCancelKeepsCommittedQuery(t *testing.T) {
	s := searchState{query: "kept"}
	s.begin()
	s.append('x')
	s.cancel()
	if s.query != "kept" {
		t.Errorf("query = %q, want the committed one unchanged", s.query)
	}
	if s.typing {
		t.Error("cancel should close the prompt")
	}
}

func TestSearchBackspaceMultibyte(t *testing.T) {
	var s searchState
	s.begin()
	s.append('é')
	s.append('ü')
	s.backspace()
	if s.draft != "é" {
		t.Errorf("draft = %q, want one whole rune removed", s.draft)
	}
}

func TestSearchStepFromPosition(t *testing.T) {
	s := searchState{query: "x", matches: []int{5, 10, 20, 40}, index: -1}

	// The first jump starts from where the reader is, not from the top.
	if got, ok := s.step(1, 12); !ok || got != 20 {
		t.Errorf("forward from line 12 gave %d, %v; want 20", got, ok)
	}
	if got, ok := s.step(1, 0); !ok || got != 40 {
		t.Errorf("next gave %d, %v; want 40", got, ok)
	}
	// Reaching the end wraps to the beginning.
	if got, ok := s.step(1, 0); !ok || got != 5 {
		t.Errorf("wrapping gave %d, %v; want 5", got, ok)
	}
	// And backwards wraps the other way.
	if got, ok := s.step(-1, 0); !ok || got != 40 {
		t.Errorf("backward wrap gave %d, %v; want 40", got, ok)
	}
}

func TestSearchStepBackwardFirst(t *testing.T) {
	s := searchState{query: "x", matches: []int{5, 10, 20}, index: -1}
	if got, ok := s.step(-1, 15); !ok || got != 10 {
		t.Errorf("backward from line 15 gave %d, %v; want 10", got, ok)
	}
}

func TestSearchStepNoMatches(t *testing.T) {
	var s searchState
	if _, ok := s.step(1, 0); ok {
		t.Error("stepping with no matches should report failure")
	}
}

func TestSearchHighlightQueryFollowsDraft(t *testing.T) {
	s := searchState{query: "old"}
	if got := s.highlightQuery(); got != "old" {
		t.Errorf("got %q, want the committed query when not typing", got)
	}
	s.begin()
	s.draft = "new"
	// While typing, matches should follow what is being typed so they appear
	// as the reader goes.
	if got := s.highlightQuery(); got != "new" {
		t.Errorf("got %q, want the draft while typing", got)
	}
}

func TestSearchActive(t *testing.T) {
	var s searchState
	if s.active() {
		t.Error("a fresh search should not be active")
	}
	s.begin()
	if s.active() {
		t.Error("an empty prompt should not highlight anything")
	}
	s.append('a')
	if !s.active() {
		t.Error("typing should make the search active")
	}
}
