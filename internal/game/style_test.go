package game

import "testing"

func TestNormalizeStyle(t *testing.T) {
	for _, input := range []string{"", "china", "japan", "korea"} {
		want := input
		if want == "" {
			want = "china"
		}
		if got, err := NormalizeStyle(input); err != nil || got != want {
			t.Fatalf("NormalizeStyle(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"unknown", "China", " japan "} {
		if _, err := NormalizeStyle(input); err == nil {
			t.Fatalf("unsupported style %q was accepted", input)
		}
	}
}

func TestCPUStylesMakeDistinctChoices(t *testing.T) {
	// Against the same center stone: influence on the fourth line, territory
	// at a third-line corner, or direct contact with the opponent.
	for _, tc := range []struct {
		style string
		want  Move
	}{
		{"china", Move{3, 3}},
		{"japan", Move{2, 2}},
		{"korea", Move{4, 3}},
	} {
		t.Run(tc.style, func(t *testing.T) {
			g := newGame(t)
			g.Style = tc.style
			g.Next = White
			place(g, Black, Move{4, 4})
			if got := g.ChooseMove(); got != tc.want {
				t.Fatalf("style %s chose %v, want %v", tc.style, got, tc.want)
			}
			if again := g.ChooseMove(); again != tc.want {
				t.Fatalf("same position produced a different choice: %v", again)
			}
			play(t, g, tc.want)
		})
	}
}

func TestCPUStylesRespectKo(t *testing.T) {
	for _, style := range []string{"china", "japan", "korea"} {
		t.Run(style, func(t *testing.T) {
			g := newGame(t)
			g.Style = style
			place(g, Black, Move{0, 1}, Move{1, 0}, Move{2, 1})
			place(g, White, Move{1, 1}, Move{0, 2}, Move{2, 2}, Move{1, 3})
			play(t, g, Move{1, 2})
			move := g.ChooseMove()
			if move == (Move{1, 1}) {
				t.Fatal("CPU tried to recapture a ko immediately")
			}
			play(t, g, move)
		})
	}
}
