package game

import (
	"reflect"
	"strconv"
	"testing"
)

func snapbackGame(t testing.TB) *Game {
	t.Helper()
	g, err := New(9)
	if err != nil {
		t.Fatal(err)
	}
	place(g, Black, Move{1, 0}, Move{1, 1})
	place(g, White, Move{0, 0}, Move{2, 0}, Move{2, 1}, Move{1, 2}, Move{0, 2})
	return g
}

func TestSearchAvoidsSnapback(t *testing.T) {
	g := snapbackGame(t)
	ranked := g.rankMoves("china")
	poisoned := Move{0, 1}
	if ranked[0].move != poisoned {
		t.Fatalf("fixture must tempt the greedy opponent, got %v", ranked[0].move)
	}
	branch := *g
	branch.Moves = nil
	play(t, &branch, poisoned)
	play(t, &branch, Move{0, 0})
	if branch.Captures.White != 3 {
		t.Fatal("fixture must give the opponent a three-stone snapback")
	}
	for _, budget := range []int{320, 1200} {
		move, stats := g.search(ranked, budget)
		if stats.depth < 2 {
			t.Fatalf("budget %d did not finish an opponent-reply search: %+v", budget, stats)
		}
		if move == poisoned {
			t.Fatalf("budget %d overlooked the snapback", budget)
		}
		if _, _, err := g.tryMove(move); err != nil {
			t.Fatalf("search selected an illegal alternative: %v", err)
		}
	}
}

func TestSearchBudgetAndIsolation(t *testing.T) {
	g := snapbackGame(t)
	// A shallow Game copy followed by Play would overwrite this spare slot.
	g.Moves = make([]Move, 1, 16)
	g.Moves[0] = Move{4, 4}
	backing := g.Moves[:cap(g.Moves)]
	for i := 1; i < len(backing); i++ {
		backing[i] = Move{99, 99}
	}
	before := *g
	before.Board = append([]int(nil), g.Board...)
	before.Moves = append([]Move(nil), g.Moves...)
	beforeBacking := append([]Move(nil), backing...)
	ranked := g.rankMoves("china")
	for _, budget := range []int{0, 1, 30, 320, 1200} {
		move, stats := g.search(ranked, budget)
		if stats.nodes > budget {
			t.Fatalf("expanded %d nodes with budget %d", stats.nodes, budget)
		}
		if repeated := g.searchMove(ranked, budget); move != repeated {
			t.Fatalf("search is not deterministic: %v vs %v", move, repeated)
		}
		if !reflect.DeepEqual(g, &before) || !reflect.DeepEqual(backing, beforeBacking) {
			t.Fatal("search mutated the live position or move-history backing array")
		}
	}
}

func TestSearchPassAndGameOver(t *testing.T) {
	g := newGame(t)
	for i := range g.Board {
		g.Board[i] = Black
	}
	place(g, Empty, Move{2, 2}, Move{6, 6})
	if move := g.searchMove(g.rankMoves("china"), 1200); move != (Move{-1, -1}) {
		t.Fatalf("settled search should pass, got %v", move)
	}
	g.Resign()
	if move := g.searchMove([]candidate{{move: Move{2, 2}}}, 1200); move != (Move{-1, -1}) {
		t.Fatalf("finished search should pass, got %v", move)
	}
}

func searchMidgame(t testing.TB, size int) *Game {
	t.Helper()
	g, err := New(size)
	if err != nil {
		t.Fatal(err)
	}
	for turn := 0; turn < size*size/3 && !g.Over; turn++ {
		ranked := g.rankMoves("korea")
		move := Move{-1, -1}
		if len(ranked) > 0 {
			move = ranked[0].move
		}
		if err := g.Play(move); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

func TestSearchSupportedBoards(t *testing.T) {
	for _, size := range []int{9, 13, 19} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			g := searchMidgame(t, size)
			move, stats := g.search(g.rankMoves("china"), 1200)
			if stats.nodes > 1200 || stats.depth < 2 {
				t.Fatalf("unexpected search work: %+v", stats)
			}
			t.Logf("nodes=%d, completed depth=%d, move=%v", stats.nodes, stats.depth, move)
			play(t, g, move)
		})
	}
}

func BenchmarkSearch19(b *testing.B) {
	g := searchMidgame(b, 19)
	ranked := g.rankMoves("china")
	for _, budget := range []int{320, 1200} {
		b.Run(strconv.Itoa(budget), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				g.searchMove(ranked, budget)
			}
		})
	}
}
