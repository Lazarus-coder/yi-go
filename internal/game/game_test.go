package game

import (
	"reflect"
	"strconv"
	"testing"
)

func newGame(t *testing.T) *Game {
	t.Helper()
	g, err := New(9)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func place(g *Game, color int, moves ...Move) {
	for _, move := range moves {
		g.Board[move.Y*g.Size+move.X] = color
	}
}

func play(t *testing.T, g *Game, move Move) {
	t.Helper()
	if err := g.Play(move); err != nil {
		t.Fatalf("Play(%v): %v", move, err)
	}
}

func TestNew(t *testing.T) {
	for _, size := range []int{9, 13, 19} {
		g, err := New(size)
		if err != nil || len(g.Board) != size*size || g.Next != Black || g.Moves == nil || g.Style != "china" {
			t.Fatalf("New(%d) = %+v, %v", size, g, err)
		}
	}
	if _, err := New(10); err == nil {
		t.Fatal("unsupported board size was accepted")
	}
}

func TestCaptureGroup(t *testing.T) {
	g := newGame(t)
	place(g, White, Move{1, 1}, Move{2, 1})
	place(g, Black, Move{0, 1}, Move{1, 0}, Move{2, 0}, Move{3, 1}, Move{1, 2})

	play(t, g, Move{2, 2})
	if g.Board[1*g.Size+1] != Empty || g.Board[1*g.Size+2] != Empty {
		t.Fatal("the whole captured group should be removed")
	}
	if g.Captures.Black != 2 || g.Next != White {
		t.Fatalf("unexpected captures or next player: %+v", g)
	}
}

func TestIllegalMovesLeaveGameUnchanged(t *testing.T) {
	g := newGame(t)
	place(g, White, Move{0, 1}, Move{1, 0}, Move{2, 1}, Move{1, 2})
	before := append([]int(nil), g.Board...)

	for _, move := range []Move{{1, 1}, {0, 1}, {-1, 2}, {9, 0}} {
		if err := g.Play(move); err == nil {
			t.Errorf("illegal move %v was accepted", move)
		}
		if !reflect.DeepEqual(before, g.Board) || g.Next != Black ||
			len(g.Moves) != 0 || g.Captures != (Captures{}) {
			t.Fatal("an invalid move changed the game")
		}
	}
}

func TestCaptureBeforeCheckingSuicide(t *testing.T) {
	g := newGame(t)
	place(g, White, Move{0, 1}, Move{1, 0})
	place(g, Black, Move{1, 1}, Move{0, 2}, Move{2, 0})

	play(t, g, Move{0, 0})
	if g.Captures.Black != 2 {
		t.Fatalf("capture should make this surrounded move legal, got %+v", g.Captures)
	}
}

func TestSimpleKo(t *testing.T) {
	g := newGame(t)
	place(g, Black, Move{0, 1}, Move{1, 0}, Move{2, 1})
	place(g, White, Move{1, 1}, Move{0, 2}, Move{2, 2}, Move{1, 3})

	play(t, g, Move{1, 2})
	if err := g.Play(Move{1, 1}); err == nil {
		t.Fatal("immediate ko recapture was accepted")
	}
	play(t, g, Move{6, 6})
	play(t, g, Move{7, 6})
	play(t, g, Move{1, 1})
	if g.Captures.White != 1 {
		t.Fatal("recapture should be legal after playing elsewhere")
	}
}

func TestPassAndGameOver(t *testing.T) {
	g := newGame(t)
	play(t, g, Move{-1, -1})
	play(t, g, Move{4, 4})
	if g.Passes != 0 {
		t.Fatal("a stone should reset consecutive passes")
	}
	play(t, g, Move{-1, -1})
	play(t, g, Move{-1, -1})
	if !g.Over || g.Score == nil || g.LastMove != nil {
		t.Fatal("two consecutive passes should end and score the game")
	}
	if err := g.Play(Move{0, 0}); err == nil {
		t.Fatal("a move after game over was accepted")
	}
}

func TestAreaScoring(t *testing.T) {
	g := newGame(t)
	for y := 0; y < g.Size; y++ {
		for x := 0; x < g.Size; x++ {
			if x < 4 {
				place(g, Black, Move{x, y})
			} else if x > 4 {
				place(g, White, Move{x, y})
			}
		}
	}
	// These enclosed empty points are territory; the center column is neutral.
	place(g, Empty, Move{1, 1}, Move{7, 7})
	play(t, g, Move{-1, -1})
	play(t, g, Move{-1, -1})
	want := &Score{Black: 36, White: 42.5, Winner: White, Margin: 6.5}
	if !reflect.DeepEqual(g.Score, want) {
		t.Fatalf("score = %+v, want %+v", g.Score, want)
	}
}

func TestResign(t *testing.T) {
	g := newGame(t)
	g.Resign()
	if !g.Over || g.Resigned != Black || g.Score.Winner != White || g.Score.Margin != 0 {
		t.Fatalf("unexpected resignation: %+v", g)
	}
}

func TestCPUTakesCapture(t *testing.T) {
	for _, style := range []string{"china", "japan", "korea"} {
		t.Run(style, func(t *testing.T) {
			g := newGame(t)
			g.Style = style
			g.Next = White
			place(g, Black, Move{1, 1}, Move{2, 1})
			place(g, White, Move{0, 1}, Move{1, 0}, Move{2, 0}, Move{3, 1}, Move{1, 2})
			move := g.ChooseMove()
			if move != (Move{2, 2}) {
				t.Fatalf("CPU chose %v instead of capturing two stones", move)
			}
			play(t, g, move)
		})
	}
}

func TestCPUSavesAtari(t *testing.T) {
	for _, style := range []string{"china", "japan", "korea"} {
		t.Run(style, func(t *testing.T) {
			g := newGame(t)
			g.Style = style
			g.Next = White
			place(g, White, Move{4, 4}, Move{4, 5})
			place(g, Black, Move{3, 4}, Move{4, 3}, Move{5, 4}, Move{3, 5}, Move{5, 5})
			if move := g.ChooseMove(); move != (Move{4, 6}) {
				t.Fatalf("CPU chose %v instead of saving its group", move)
			}
		})
	}
}

func TestCPUProtectsEyesAndPasses(t *testing.T) {
	for _, style := range []string{"china", "japan", "korea"} {
		t.Run(style, func(t *testing.T) {
			g := newGame(t)
			g.Style = style
			for point := range g.Board {
				g.Board[point] = Black
			}
			place(g, Empty, Move{2, 2}, Move{6, 6})
			if move := g.ChooseMove(); move != (Move{-1, -1}) {
				t.Fatalf("CPU should preserve both eyes and pass, chose %v", move)
			}
		})
	}
}

func TestCPUPlaysLegalMovesAndFinishes(t *testing.T) {
	for _, style := range []string{"china", "japan", "korea"} {
		for _, size := range []int{9, 13, 19} {
			t.Run(style+"/"+strconv.Itoa(size), func(t *testing.T) {
				g, err := New(size)
				if err != nil {
					t.Fatal(err)
				}
				g.Style = style
				for turn := 0; turn < size*size*3 && !g.Over; turn++ {
					before := append([]int(nil), g.Board...)
					next, moves := g.Next, len(g.Moves)
					move := g.ChooseMove()
					if !reflect.DeepEqual(before, g.Board) || g.Next != next || len(g.Moves) != moves {
						t.Fatal("thinking changed the game")
					}
					play(t, g, move)
				}
				if !g.Over {
					t.Fatal("CPU self-play should settle and finish")
				}
			})
		}
	}
}
