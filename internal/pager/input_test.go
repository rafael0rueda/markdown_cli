package pager

import "testing"

func TestDecodeKeySimple(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want key
		n    int
	}{
		{"letter", "j", key{Rune: 'j'}, 1},
		{"space", " ", key{Rune: ' '}, 1},
		{"slash", "/", key{Rune: '/'}, 1},
		{"enter", "\r", key{Name: keyEnter}, 1},
		{"newline", "\n", key{Name: keyEnter}, 1},
		{"backspace", "\x7f", key{Name: keyBackspace}, 1},
		{"ctrl-c", "\x03", key{Rune: 'c', Ctrl: true}, 1},
		{"ctrl-d", "\x04", key{Rune: 'd', Ctrl: true}, 1},
		{"multibyte rune", "é", key{Rune: 'é'}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, n, ok := decodeKey([]byte(tt.in))
			if !ok {
				t.Fatal("decode reported incomplete input")
			}
			if got != tt.want || n != tt.n {
				t.Errorf("got %+v consuming %d, want %+v consuming %d", got, n, tt.want, tt.n)
			}
		})
	}
}

func TestDecodeKeyEscapeSequences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want keyName
	}{
		{"up", "\x1b[A", keyUp},
		{"down", "\x1b[B", keyDown},
		{"right", "\x1b[C", keyRight},
		{"left", "\x1b[D", keyLeft},
		{"home", "\x1b[H", keyHome},
		{"end", "\x1b[F", keyEnd},
		{"page up", "\x1b[5~", keyPageUp},
		{"page down", "\x1b[6~", keyPageDown},
		{"home numbered", "\x1b[1~", keyHome},
		{"end numbered", "\x1b[4~", keyEnd},
		{"application up", "\x1bOA", keyUp},
		{"modified up", "\x1b[1;5A", keyUp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, n, ok := decodeKey([]byte(tt.in))
			if !ok {
				t.Fatal("decode reported incomplete input")
			}
			if got.Name != tt.want {
				t.Errorf("got %v, want %v", got.Name, tt.want)
			}
			if n != len(tt.in) {
				t.Errorf("consumed %d bytes, want %d", n, len(tt.in))
			}
		})
	}
}

// TestDecodeKittyKeyboard covers the protocol the pager enables when the
// terminal supports it. Escape is the reason for using it: in the legacy
// encoding it cannot be told apart from the start of an arrow key without
// waiting for a timeout.
func TestDecodeKittyKeyboard(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want key
	}{
		{"escape", "\x1b[27u", key{Name: keyEscape}},
		{"escape with modifiers", "\x1b[27;1u", key{Name: keyEscape}},
		{"enter", "\x1b[13u", key{Name: keyEnter}},
		{"backspace", "\x1b[127u", key{Name: keyBackspace}},
		{"letter", "\x1b[106u", key{Rune: 'j'}},
		{"ctrl-c", "\x1b[99;5u", key{Rune: 'c', Ctrl: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, n, ok := decodeKey([]byte(tt.in))
			if !ok {
				t.Fatal("decode reported incomplete input")
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
			if n != len(tt.in) {
				t.Errorf("consumed %d bytes, want %d", n, len(tt.in))
			}
		})
	}
}

// TestDecodeKeyIncomplete is what makes reading robust: an escape sequence can
// be split across two reads, and decoding half of one must not invent a key.
func TestDecodeKeyIncomplete(t *testing.T) {
	for _, in := range []string{"", "\x1b[", "\x1b[5", "\x1b[1;5", "\x1bO", "\xc3"} {
		if _, _, ok := decodeKey([]byte(in)); ok {
			t.Errorf("%q was decoded as complete", in)
		}
	}
}

func TestDecodeLoneEscape(t *testing.T) {
	got, n, ok := decodeKey([]byte{0x1b})
	if !ok || got.Name != keyEscape || n != 1 {
		t.Errorf("got %+v, %d, %v; want escape consuming 1 byte", got, n, ok)
	}
}

// TestDecodeKeyStream checks that several keypresses arriving in one read are
// all recovered, which is what happens when a key is held down.
func TestDecodeKeyStream(t *testing.T) {
	buf := []byte("jj\x1b[Bk\x1b[6~q")
	var got []key
	for len(buf) > 0 {
		k, n, ok := decodeKey(buf)
		if !ok {
			t.Fatalf("incomplete at %q", buf)
		}
		got = append(got, k)
		buf = buf[n:]
	}
	want := []key{
		{Rune: 'j'}, {Rune: 'j'}, {Name: keyDown}, {Rune: 'k'},
		{Name: keyPageDown}, {Rune: 'q'},
	}
	if len(got) != len(want) {
		t.Fatalf("decoded %d keys, want %d: %+v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("key %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
