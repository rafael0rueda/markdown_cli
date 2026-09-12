//go:build linux

package pager

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/rafael0rueda/markdown_cli/internal/render"
	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// openPTY returns a connected pseudo-terminal pair sized to cols by rows.
//
// The slave end is a real terminal as far as the kernel is concerned, so raw
// mode, the window size and reads all behave as they would against a user's
// terminal. That is the point: it drives the pager's actual I/O rather than a
// mock of it.
func openPTY(t *testing.T, cols, rows int) (master, slave *os.File) {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("cannot open /dev/ptmx: %v", err)
	}
	t.Cleanup(func() { master.Close() })

	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlocking pty: %v", err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("getting pty number: %v", err)
	}

	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("opening pty slave: %v", err)
	}
	t.Cleanup(func() { slave.Close() })

	size := unix.Winsize{Row: uint16(rows), Col: uint16(cols)}
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &size); err != nil {
		t.Fatalf("setting window size: %v", err)
	}
	return master, slave
}

// session is a running pager plus the terminal it is drawing on.
type session struct {
	t      *testing.T
	master *os.File
	slave  *os.File
	done   chan error

	mu       sync.Mutex
	term     *vterm
	raw      strings.Builder
	lastRead time.Time
}

// startSession runs the pager over the given markdown.
func startSession(t *testing.T, source string, cols, rows int, opts *Options) *session {
	t.Helper()
	master, slave := openPTY(t, cols, rows)

	options := Options{
		Render: func(width int) (*render.Doc, error) {
			return render.Render([]byte(source), render.Options{
				Width: width, Theme: theme.Plain(),
			})
		},
		Write: render.WriteOptions{Color: theme.ColorNone},
		Theme: theme.Plain(),
		Title: "test.md",
	}
	if opts != nil {
		if opts.Render != nil {
			options.Render = opts.Render
		}
		options.Images = opts.Images
		options.MaxWidth = opts.MaxWidth
	}

	s := &session{
		t: t, master: master, slave: slave,
		done: make(chan error, 1),
		term: newVTerm(cols, rows),
	}

	// Drain the master continuously; a full pty buffer would block the pager
	// mid-frame and deadlock the test. Everything read is fed through the
	// emulator, so the grid always reflects the latest frame.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.term.write(buf[:n])
				s.raw.Write(buf[:n])
				s.lastRead = time.Now()
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	scr, err := newScreen(slave, false)
	if err != nil {
		t.Fatalf("opening screen: %v", err)
	}
	scr.keepTTYOpen()

	go func() { s.done <- run(scr, options) }()
	s.settle()
	return s
}

// send types keys into the terminal and waits for the frame they produce.
func (s *session) send(keys string) {
	s.t.Helper()
	if _, err := s.master.WriteString(keys); err != nil {
		s.t.Fatalf("writing keys: %v", err)
	}
	s.settle()
}

// settle waits until the pager has stopped writing.
//
// Waiting for the output to go quiet rather than for a fixed delay keeps the
// tests from being flaky on a loaded machine, and keeps them quick when it is
// not.
func (s *session) settle() {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
		s.mu.Lock()
		idle := !s.lastRead.IsZero() && time.Since(s.lastRead) > 40*time.Millisecond
		s.mu.Unlock()
		if idle {
			return
		}
	}
}

// screen returns what is on the terminal, as text.
func (s *session) screen() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.term.String()
}

// status returns the pager's bottom bar.
func (s *session) status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.term.statusLine()
}

// rawOutput returns every byte the pager has written, escape sequences and all.
func (s *session) rawOutput() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw.String()
}

// resize changes the window size and tells the process, as a terminal would.
func (s *session) resize(cols, rows int) {
	s.t.Helper()
	size := unix.Winsize{Row: uint16(rows), Col: uint16(cols)}
	if err := unix.IoctlSetWinsize(int(s.slave.Fd()), unix.TIOCSWINSZ, &size); err != nil {
		s.t.Skipf("cannot resize pty: %v", err)
	}
	s.mu.Lock()
	s.term.resize(cols, rows)
	s.mu.Unlock()

	unix.Kill(unix.Getpid(), unix.SIGWINCH)
	s.settle()
}

// quit leaves any prompt, sends q, and waits for the pager to return.
func (s *session) quit() error {
	s.master.WriteString("\x1b")
	time.Sleep(30 * time.Millisecond)
	s.master.WriteString("q")
	select {
	case err := <-s.done:
		return err
	case <-time.After(3 * time.Second):
		s.t.Fatal("pager did not exit after q")
		return nil
	}
}

// numberedDoc builds a document whose every line names itself, so a test can
// tell exactly which part of it is on screen.
func numberedDoc(lines int) string {
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "Line %03d of the document\n\n", i)
	}
	return b.String()
}

func TestPagerShowsTheTopOfTheDocument(t *testing.T) {
	s := startSession(t, numberedDoc(100), 60, 12, nil)
	defer s.quit()

	got := s.screen()
	if !strings.Contains(got, "Line 001") {
		t.Errorf("the first line is not on screen:\n%s", got)
	}
	if strings.Contains(got, "Line 050") {
		t.Errorf("a line far down the document was drawn:\n%s", got)
	}
	if !strings.Contains(got, "test.md") {
		t.Errorf("the status line is missing:\n%s", got)
	}
}

func TestPagerScrolls(t *testing.T) {
	s := startSession(t, numberedDoc(100), 60, 12, nil)
	defer s.quit()

	s.send("jjjj")
	got := s.screen()
	if strings.Contains(got, "Line 001") {
		t.Errorf("the top line is still shown after scrolling:\n%s", got)
	}
	if !strings.Contains(got, "Line 003") {
		t.Errorf("expected to have scrolled to line 3:\n%s", got)
	}

	s.send("kkkk")
	if got := s.screen(); !strings.Contains(got, "Line 001") {
		t.Errorf("scrolling back up did not return to the top:\n%s", got)
	}
}

func TestPagerArrowKeysScroll(t *testing.T) {
	s := startSession(t, numberedDoc(100), 60, 12, nil)
	defer s.quit()

	s.send("\x1b[B\x1b[B\x1b[B\x1b[B")
	if got := s.screen(); strings.Contains(got, "Line 001") {
		t.Errorf("the down arrow did not scroll:\n%s", got)
	}
}

func TestPagerJumpsToEndAndBack(t *testing.T) {
	s := startSession(t, numberedDoc(60), 60, 12, nil)
	defer s.quit()

	s.send("G")
	if got := s.screen(); !strings.Contains(got, "Line 060") {
		t.Errorf("G did not reach the end:\n%s", got)
	}
	if got := s.status(); !strings.Contains(got, "end") {
		t.Errorf("the status line should say the view is at the end, got %q", got)
	}

	s.send("g")
	if got := s.screen(); !strings.Contains(got, "Line 001") {
		t.Errorf("g did not return to the top:\n%s", got)
	}
}

// TestPagerCannotScrollPastTheEnds is what keeps the view from drifting into
// blank space above or below the document.
func TestPagerCannotScrollPastTheEnds(t *testing.T) {
	s := startSession(t, numberedDoc(6), 60, 12, nil)
	defer s.quit()

	s.send(strings.Repeat("k", 20))
	if got := s.screen(); !strings.Contains(got, "Line 001") {
		t.Errorf("scrolling up past the top lost the first line:\n%s", got)
	}

	s.send(strings.Repeat("j", 40))
	if got := s.screen(); !strings.Contains(got, "Line 006") {
		t.Errorf("scrolling down past the end lost the last line:\n%s", got)
	}
}

func TestPagerPageDown(t *testing.T) {
	s := startSession(t, numberedDoc(100), 60, 12, nil)
	defer s.quit()

	s.send(" ")
	got := s.screen()
	if strings.Contains(got, "Line 001") {
		t.Errorf("space did not page forward:\n%s", got)
	}

	s.send("\x1b[5~")
	if got := s.screen(); !strings.Contains(got, "Line 001") {
		t.Errorf("page up did not return to the top:\n%s", got)
	}
}

func TestPagerSearch(t *testing.T) {
	s := startSession(t, numberedDoc(100), 60, 12, nil)
	defer s.quit()

	s.send("/Line 042\r")
	if got := s.screen(); !strings.Contains(got, "Line 042") {
		t.Errorf("search did not jump to the match:\n%s", got)
	}
	if got := s.status(); !strings.Contains(got, "match 1 of") {
		t.Errorf("the status line should report the match, got %q", got)
	}
}

func TestPagerSearchPromptIsShown(t *testing.T) {
	s := startSession(t, numberedDoc(100), 60, 12, nil)
	defer s.quit()

	s.send("/abc")
	// The prompt has to echo what is being typed, not the previous query.
	if got := s.status(); !strings.Contains(got, "/abc") {
		t.Errorf("the search prompt does not show what was typed, got %q", got)
	}
}

func TestPagerSearchCancelled(t *testing.T) {
	s := startSession(t, numberedDoc(100), 60, 12, nil)
	defer s.quit()

	s.send("/Line 042")
	s.send("\x1b")

	if got := s.status(); strings.Contains(got, "/Line 042") {
		t.Errorf("escape did not close the prompt, got %q", got)
	}
	// Cancelling must not move the view.
	if got := s.screen(); !strings.Contains(got, "Line 001") {
		t.Errorf("cancelling a search moved the view:\n%s", got)
	}
}

func TestPagerSearchNextAndPrevious(t *testing.T) {
	s := startSession(t, "alpha\n\nbeta\n\nalpha\n\ngamma\n\nalpha\n", 60, 6, nil)
	defer s.quit()

	s.send("/alpha\r")
	if got := s.status(); !strings.Contains(got, "match 1 of 3") {
		t.Errorf("expected three matches, got %q", got)
	}

	s.send("n")
	if got := s.status(); !strings.Contains(got, "match 2 of 3") {
		t.Errorf("n did not advance, got %q", got)
	}

	s.send("N")
	if got := s.status(); !strings.Contains(got, "match 1 of 3") {
		t.Errorf("N did not go back, got %q", got)
	}
}

func TestPagerSearchNoMatch(t *testing.T) {
	s := startSession(t, numberedDoc(20), 60, 12, nil)
	defer s.quit()

	s.send("/nothing here\r")
	if got := s.status(); !strings.Contains(got, "no match") {
		t.Errorf("a failed search should say so, got %q", got)
	}
}

func TestPagerSearchIsCaseInsensitive(t *testing.T) {
	s := startSession(t, "First\n\nSECOND\n\nthird\n", 60, 8, nil)
	defer s.quit()

	s.send("/second\r")
	if got := s.status(); !strings.Contains(got, "match 1 of 1") {
		t.Errorf("search should ignore case, got %q", got)
	}
}

// TestPagerResizeReflows checks that the document is laid out again at the new
// width. Wrapping, table widths and image sizes all depend on it.
func TestPagerResizeReflows(t *testing.T) {
	long := strings.Repeat("word ", 40)
	s := startSession(t, long, 80, 12, nil)
	defer s.quit()

	s.resize(30, 12)

	for i, line := range strings.Split(s.screen(), "\n") {
		if len([]rune(line)) > 30 {
			t.Errorf("row %d is %d columns wide in a 30 column window: %q",
				i, len([]rune(line)), line)
			break
		}
	}
}

func TestPagerQuitsCleanly(t *testing.T) {
	s := startSession(t, numberedDoc(10), 60, 12, nil)
	if err := s.quit(); err != nil {
		t.Errorf("pager returned an error: %v", err)
	}
}

func TestPagerQuitsOnCtrlC(t *testing.T) {
	s := startSession(t, numberedDoc(10), 60, 12, nil)
	s.master.WriteString("\x03")
	select {
	case err := <-s.done:
		if err != nil {
			t.Errorf("pager returned an error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ctrl-c did not quit the pager")
	}
}

// TestPagerRestoresTheTerminal is what keeps the shell usable afterwards.
func TestPagerRestoresTheTerminal(t *testing.T) {
	master, slave := openPTY(t, 60, 12)
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := master.Read(buf); err != nil {
				return
			}
		}
	}()

	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Skipf("cannot read termios: %v", err)
	}

	scr, err := newScreen(slave, true)
	if err != nil {
		t.Fatal(err)
	}
	scr.keepTTYOpen()

	done := make(chan error, 1)
	go func() {
		done <- run(scr, Options{
			Render: func(width int) (*render.Doc, error) {
				return render.Render([]byte("hello"), render.Options{Width: width, Theme: theme.Plain()})
			},
			Theme: theme.Plain(),
		})
	}()

	time.Sleep(80 * time.Millisecond)
	master.WriteString("q")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pager did not exit")
	}

	after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if *before != *after {
		t.Errorf("terminal left in a different mode\nbefore: %+v\nafter:  %+v", before, after)
	}
}

// TestPagerLeavesTheAlternateScreen checks the escape sequences that make the
// document disappear on exit, leaving the shell as it was.
func TestPagerScreenSetupAndTeardown(t *testing.T) {
	s := startSession(t, numberedDoc(10), 60, 12, nil)
	if err := s.quit(); err != nil {
		t.Fatal(err)
	}
	s.settle()

	raw := s.rawOutput()
	for _, want := range []string{enterAltScreen, hideCursor, showCursor, leaveAltScreen} {
		if !strings.Contains(raw, want) {
			t.Errorf("output is missing %q", want)
		}
	}
	if strings.Index(raw, enterAltScreen) > strings.Index(raw, leaveAltScreen) {
		t.Error("the alternate screen was left before it was entered")
	}
}

// --- images ---

// fakeDrawer records how the pager asks for images to be drawn.
type fakeDrawer struct {
	mu    sync.Mutex
	calls []drawCall
}

type drawCall struct {
	ref           string
	cols, rows    int
	skip, visible int
}

func (f *fakeDrawer) ClearPlacements() string { return "" }

func (f *fakeDrawer) DrawCropped(ref string, cols, rows, skip, visible, indent int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, drawCall{ref, cols, rows, skip, visible})
	return "", nil // nothing drawn, so the emulator's grid is unaffected
}

// last returns the most recent call, if any.
func (f *fakeDrawer) last() (drawCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return drawCall{}, false
	}
	return f.calls[len(f.calls)-1], true
}

func (f *fakeDrawer) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

// imageDoc builds a document with one image of the given height, preceded by
// some lines of text.
func imageDoc(before, rows int) Renderer {
	return func(width int) (*render.Doc, error) {
		doc := &render.Doc{Width: width}
		for i := 1; i <= before; i++ {
			doc.Lines = append(doc.Lines, render.Line{
				Runs: []render.Run{{Text: fmt.Sprintf("Line %03d", i)}},
			})
		}
		start := len(doc.Lines)
		for i := 0; i < rows; i++ {
			doc.Lines = append(doc.Lines, render.Line{})
		}
		doc.Images = append(doc.Images, render.Image{
			Ref: "pic.png", Line: start, Cols: 20, Rows: rows,
		})
		for i := 1; i <= 30; i++ {
			doc.Lines = append(doc.Lines, render.Line{
				Runs: []render.Run{{Text: fmt.Sprintf("After %03d", i)}},
			})
		}
		return doc, nil
	}
}

// TestPagerDrawsVisibleImage covers the ordinary case: an image entirely on
// screen is drawn whole.
func TestPagerDrawsVisibleImage(t *testing.T) {
	drawer := &fakeDrawer{}
	s := startSession(t, "", 60, 20, &Options{Render: imageDoc(2, 6), Images: drawer})
	defer s.quit()

	call, ok := drawer.last()
	if !ok {
		t.Fatal("the image was never drawn")
	}
	if call.ref != "pic.png" {
		t.Errorf("ref = %q", call.ref)
	}
	if call.skip != 0 || call.visible != 6 {
		t.Errorf("skip=%d visible=%d, want 0 and 6 for a fully visible image",
			call.skip, call.visible)
	}
}

// TestPagerCropsImageScrolledOffTheTop is what makes scrolling through a
// picture continuous rather than having it vanish the moment its first row
// leaves the screen.
func TestPagerCropsImageScrolledOffTheTop(t *testing.T) {
	drawer := &fakeDrawer{}
	s := startSession(t, "", 60, 20, &Options{Render: imageDoc(2, 8), Images: drawer})
	defer s.quit()

	// The image starts at line 2; scrolling down 5 hides its first three rows.
	drawer.reset()
	s.send("jjjjj")

	call, ok := drawer.last()
	if !ok {
		t.Fatal("the image stopped being drawn once partly scrolled away")
	}
	if call.skip != 3 {
		t.Errorf("skip = %d, want 3 rows hidden above the view", call.skip)
	}
	if call.visible != 5 {
		t.Errorf("visible = %d, want the remaining 5 rows", call.visible)
	}
	// The footprint stays the whole image; only the crop changes.
	if call.rows != 8 {
		t.Errorf("rows = %d, want the image's full height 8", call.rows)
	}
}

// TestPagerCropsImageAtTheBottom covers the other edge: an image running past
// the last visible row.
func TestPagerCropsImageAtTheBottom(t *testing.T) {
	drawer := &fakeDrawer{}
	// A 10-row image starting at line 6, in a window with 7 usable rows.
	s := startSession(t, "", 60, 8, &Options{Render: imageDoc(6, 10), Images: drawer})
	defer s.quit()

	call, ok := drawer.last()
	if !ok {
		t.Fatal("the image was never drawn")
	}
	if call.skip != 0 {
		t.Errorf("skip = %d, want 0", call.skip)
	}
	// Only the row available below the text can be used.
	if call.visible < 1 || call.visible > 7 {
		t.Errorf("visible = %d, which does not fit the window", call.visible)
	}
}

// TestPagerSkipsOffscreenImages keeps a long document from redrawing every
// picture in it on each frame.
func TestPagerSkipsOffscreenImages(t *testing.T) {
	drawer := &fakeDrawer{}
	s := startSession(t, "", 60, 10, &Options{Render: imageDoc(2, 4), Images: drawer})
	defer s.quit()

	// Scroll well past the image.
	drawer.reset()
	s.send("G")

	if call, ok := drawer.last(); ok {
		t.Errorf("an image far above the view was still drawn: %+v", call)
	}
}

// --- navigation ---

// sectionedDoc builds a document of headed sections, each long enough that
// one screen shows only one heading.
func sectionedDoc(sections int) string {
	var b strings.Builder
	for i := 1; i <= sections; i++ {
		fmt.Fprintf(&b, "## Section %d\n\n", i)
		for j := 1; j <= 15; j++ {
			fmt.Fprintf(&b, "Section %d, line %02d\n\n", i, j)
		}
	}
	return b.String()
}

// topLine is the first row of the screen, where a jump puts its heading.
func (s *session) topLine() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.term.line(0))
}

func TestPagerJumpsBetweenHeadings(t *testing.T) {
	s := startSession(t, sectionedDoc(4), 60, 12, nil)
	defer s.quit()

	for _, step := range []struct{ keys, want string }{
		{"]", "## Section 2"},
		{"]", "## Section 3"},
		{"[", "## Section 2"},
		{"[", "## Section 1"},
	} {
		s.send(step.keys)
		if got := s.topLine(); got != step.want {
			t.Fatalf("after %q the top line is %q, want %q", step.keys, got, step.want)
		}
	}
	s.send("[")
	if got := s.status(); !strings.Contains(got, "no earlier headings") {
		t.Errorf("status = %q", got)
	}
}

// TestPagerHeadingJumpAtTheEnd: the last heading is on screen with nowhere
// further to scroll, so ] says so instead of silently doing nothing.
func TestPagerHeadingJumpAtTheEnd(t *testing.T) {
	s := startSession(t, sectionedDoc(3), 60, 12, nil)
	defer s.quit()

	s.send("G")
	s.send("]")
	if got := s.status(); !strings.Contains(got, "no more headings") {
		t.Errorf("status = %q", got)
	}
}

func TestPagerContents(t *testing.T) {
	s := startSession(t, "# Guide\n\n"+sectionedDoc(3), 60, 12, nil)
	defer s.quit()

	s.send("t")
	screen := s.screen()
	for _, want := range []string{"Guide", "Section 1", "Section 3"} {
		if !strings.Contains(screen, want) {
			t.Errorf("contents lack %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "line 01") {
		t.Errorf("the document is still showing under the contents:\n%s", screen)
	}
	if got := s.status(); !strings.Contains(got, "Contents") {
		t.Errorf("status = %q", got)
	}
	// Sections are indented under the title they belong to.
	if !strings.Contains(screen, "   Section 1") {
		t.Errorf("second-level headings are not indented:\n%s", screen)
	}

	s.send("jjj\r") // the cursor starts on the title; three down is Section 3
	if got := s.topLine(); got != "## Section 3" {
		t.Errorf("Enter should jump to the chosen heading; top line is %q", got)
	}
}

// TestPagerContentsStartAtTheCurrentSection puts the cursor on what is being
// read, so Enter alone goes nowhere unexpected.
func TestPagerContentsStartAtTheCurrentSection(t *testing.T) {
	s := startSession(t, sectionedDoc(4), 60, 12, nil)
	defer s.quit()

	s.send("]]j") // into section 3
	s.send("t\r")
	if got := s.topLine(); got != "## Section 3" {
		t.Errorf("top line is %q, want section 3's heading", got)
	}
}

func TestPagerContentsClose(t *testing.T) {
	s := startSession(t, sectionedDoc(2), 60, 12, nil)
	defer s.quit()

	for _, key := range []string{"\x1b", "q", "t"} {
		s.send("t")
		s.send(key)
		if got := s.screen(); !strings.Contains(got, "Section 1, line 01") {
			t.Errorf("%q should close the contents:\n%s", key, got)
		}
	}
}

func TestPagerContentsWithoutHeadings(t *testing.T) {
	s := startSession(t, numberedDoc(30), 60, 12, nil)
	defer s.quit()

	s.send("t")
	if got := s.status(); !strings.Contains(got, "no headings") {
		t.Errorf("status = %q", got)
	}
	if got := s.screen(); !strings.Contains(got, "Line 001") {
		t.Errorf("the document should still be showing:\n%s", got)
	}
}

func TestPagerHelp(t *testing.T) {
	s := startSession(t, numberedDoc(30), 70, 24, nil)
	defer s.quit()

	if got := s.status(); !strings.Contains(got, "? help") {
		t.Errorf("the status line should mention help: %q", got)
	}
	s.send("?")
	screen := s.screen()
	for _, want := range []string{"Scroll a line", "Next or previous heading", "Table of contents", "Quit"} {
		if !strings.Contains(screen, want) {
			t.Errorf("help lacks %q:\n%s", want, screen)
		}
	}

	// q closes the help rather than quitting from under it.
	s.send("q")
	select {
	case <-s.done:
		t.Fatal("q in the help quit the pager")
	default:
	}
	if got := s.screen(); !strings.Contains(got, "Line 001") {
		t.Errorf("the document should be back:\n%s", got)
	}
}

// TestPagerHelpScrollsInASmallWindow: in a window shorter than the help, the
// scrolling keys scroll it instead of closing it.
func TestPagerHelpScrollsInASmallWindow(t *testing.T) {
	s := startSession(t, numberedDoc(30), 70, 8, nil)
	defer s.quit()

	s.send("?")
	if strings.Contains(s.screen(), "Quit") {
		t.Skip("the help fits this window after all")
	}
	s.send("jjjjjjjjjjjjjjjjjjjj")
	if got := s.screen(); !strings.Contains(got, "Quit") {
		t.Errorf("scrolling should reach the end of the help:\n%s", got)
	}
	s.send("g")
	if got := s.screen(); strings.Contains(got, "Quit") || !strings.Contains(got, "Scroll a line") {
		t.Errorf("g should go back to the start of the help:\n%s", got)
	}
	s.send("x")
	if got := s.screen(); !strings.Contains(got, "Line 001") {
		t.Errorf("any other key should close the help:\n%s", got)
	}
}

func TestPagerEnablesWheelScrolling(t *testing.T) {
	s := startSession(t, numberedDoc(10), 60, 12, nil)
	if err := s.quit(); err != nil {
		t.Fatal(err)
	}
	s.settle()

	raw := s.rawOutput()
	on, off := strings.Index(raw, enableAltScroll), strings.Index(raw, restoreAltScroll)
	if on < 0 || off < 0 || off < on {
		t.Errorf("alternate scroll should be enabled on entry and restored on exit")
	}
}
