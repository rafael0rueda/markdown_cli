package render

import (
	"strings"
	"testing"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

func TestTexToUnicode(t *testing.T) {
	tests := []struct{ tex, want string }{
		{`E = mc^2`, "E = mc²"},
		{`\alpha_1 + \beta_{ij} \leq \infty`, "α₁ + βᵢⱼ ≤ ∞"},
		{`x^{n+1}`, "xⁿ⁺¹"},
		{`e^{i\pi}`, "e^(iπ)"}, // π has no raised form
		{`a_Q`, "a_Q"},
		{`\frac{a+b}{2}`, "(a+b)/2"},
		{`\frac{n(n+1)}{2}`, "n(n+1)/2"},
		{`\frac{1}{n}`, "1/n"},
		{`\sqrt{x^2 + y^2}`, "√(x² + y²)"},
		{`\sqrt[3]{8}`, "³√8"},
		{`x \in \mathbb{R}`, "x ∈ ℝ"},
		{`\mathbf{v} \cdot \vec{w}`, "v · w⃗"},
		{`90^\circ`, "90°"},
		{`f^\prime(x)`, "f′(x)"},
		{`\lim_{x \to 0} \frac{\sin x}{x}`, "lim_(x → 0) (sin x)/x"},
		{`\text{if } x > 0`, "if x > 0"},
		{`\left( a \right)`, "( a )"},
		{`a \quad b`, "a b"},
		{`100\%`, "100%"},
		// Unknown commands stay as written; readers of TeX can read them.
		{`\unknowncmd{x}{y^2}`, `\unknowncmd{x}{y²}`},
	}
	for _, tt := range tests {
		if got := texToUnicode(tt.tex, false); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.tex, got, tt.want)
		}
	}
}

func TestTexDisplayLines(t *testing.T) {
	got := texToUnicode("\\begin{aligned}\nf(x) &= x^2 \\\\\ng(x) &= 2x\n\\end{aligned}", true)
	lines := strings.Split(got, "\n")
	var kept []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			kept = append(kept, l)
		}
	}
	if strings.Join(kept, "|") != "f(x) = x²|g(x) = 2x" {
		t.Errorf("got %q", got)
	}
}

func TestInlineMath(t *testing.T) {
	tests := []struct{ src, want string }{
		{"Energy $E = mc^2$ here.", "Energy E = mc² here."},
		{"$x$ at the start", "x at the start"},
		{"Inline display $$a^2$$ too.", "Inline display a² too."},
		{"Stars and underscores $a_1 * b_2 * c$ stay math.", "Stars and underscores a₁ * b₂ * c stay math."},
		// Money is not math.
		{"It costs $5 and $10.", "It costs $5 and $10."},
		{"From $5-$10 a day.", "From $5-$10 a day."},
		{"A lone $ sign.", "A lone $ sign."},
		{"Spaced $ x $ is not math.", "Spaced $ x $ is not math."},
		{`Escaped \$20 and \$x\$.`, "Escaped $20 and $x$."},
		{"Code `$x^2$` is code.", "Code $x^2$ is code."},
		{"Empty $$ $$ is text.", "Empty $$ $$ is text."},
	}
	for _, tt := range tests {
		got := strings.ReplaceAll(plainText(t, tt.src+"\n", 80), nbsp, " ")
		if got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.src, got, tt.want)
		}
	}
}

// TestShortMathStaysOnOneLine: a formula up to half the line is kept whole,
// and a longer one wraps rather than overflowing.
func TestShortMathStaysOnOneLine(t *testing.T) {
	src := "The limit is $\\lim_{x \\to 0} \\frac{\\sin x}{x} = 1$ as shown.\n"
	const formula = "lim_(x → 0) (sin x)/x = 1" // 25 cells
	for width := 50; width <= 80; width++ {
		text := strings.ReplaceAll(plainText(t, src, width), nbsp, " ")
		if !strings.Contains(text, formula) {
			t.Errorf("width %d: formula split: %q", width, text)
		}
	}
	for width := 20; width < 50; width++ {
		for _, line := range strings.Split(plainText(t, src, width), "\n") {
			if w := uniWidth(line); w > width {
				t.Errorf("width %d: line of %d cells: %q", width, w, line)
			}
		}
	}
}

func TestDisplayMath(t *testing.T) {
	src := "Before.\n\n$$\n\\sum_{i=1}^{n} i = \\frac{n(n+1)}{2}\n$$\n\nAfter.\n"
	got := plainText(t, src, 40)
	lines := strings.Split(got, "\n")
	if len(lines) != 5 {
		t.Fatalf("got %q", got)
	}
	display := lines[2]
	if strings.TrimSpace(display) != "∑ᵢ₌₁ⁿ i = n(n+1)/2" {
		t.Errorf("display = %q", display)
	}
	// Centered: the space before it is about half of what is left over.
	pad := len(display) - len(strings.TrimLeft(display, " "))
	if want := (40 - uniWidth(strings.TrimSpace(display))) / 2; pad != want {
		t.Errorf("display indented %d, want %d to center it", pad, want)
	}

	if got := strings.TrimSpace(plainText(t, "$$ e^{i\\pi} + 1 = 0 $$\n", 40)); got != "e^(iπ) + 1 = 0" {
		t.Errorf("one-line display: %q", got)
	}
}

// TestUnclosedDisplayIsText: a $$ that never closes is shown, rather than
// taking the rest of the document with it.
func TestUnclosedDisplayIsText(t *testing.T) {
	got := plainText(t, "$$\nx^2\n\nThe rest of the document.\n", 60)
	if !strings.Contains(got, "The rest of the document.") || !strings.Contains(got, "$$") {
		t.Errorf("got %q", got)
	}
}

func TestMathStyle(t *testing.T) {
	th := theme.Dark()
	doc, err := Render([]byte("See $x^2$.\n"), Options{Width: 40, Theme: th})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range doc.Lines[0].Runs {
		if r.Text == "x²" {
			found = true
			if r.Style != th.Math {
				t.Errorf("math style = %+v, want %+v", r.Style, th.Math)
			}
		}
	}
	if !found {
		t.Errorf("no run holds the formula: %+v", doc.Lines[0].Runs)
	}
	if len(doc.Search("x²")) != 1 {
		t.Error("math should be searchable as it is shown")
	}
}
