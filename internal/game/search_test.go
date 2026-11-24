package game

import (
	"reflect"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func snapbackGame(t testing.TB) *Game {
	t.Helper()
	g, err := New(9)
	if err != nil {
		t.Fatal(err)
	}
	place(g, Black, Move{1, 0}, Move{1, 1})
	place(g, White, Move{0, 0}, Move{2, 0}, Move{2, 1}, Move{1, 2}, Move{0, 2})
	g.Style = "alphago"
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
	for _, depth := range []int{2, 3} {
		result := g.SearchShard(0, 1, depth, g.SearchConfig().NodeBudget)
		if !result.Complete {
			t.Fatalf("depth %d was not completed: %+v", depth, result)
		}
		if result.Move == poisoned {
			t.Fatalf("depth %d overlooked the snapback", depth)
		}
		if _, _, err := g.tryMove(result.Move); err != nil {
			t.Fatal(err)
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
	for _, budget := range []int{1, 8, 256, 2048} {
		result := g.SearchShard(0, 1, 3, budget)
		if result.Nodes > budget*result.TotalRoots {
			t.Fatalf("node budget exceeded: %+v", result)
		}
		if repeated := g.SearchShard(0, 1, 3, budget); result != repeated {
			t.Fatal("fixed work is not deterministic")
		}
		if !reflect.DeepEqual(g, &before) || !reflect.DeepEqual(backing, beforeBacking) {
			t.Fatal("search mutated the live position or spare move capacity")
		}
	}
}

func TestSearchPassAndGameOver(t *testing.T) {
	g := newGame(t)
	for i := range g.Board {
		g.Board[i] = Black
	}
	place(g, Empty, Move{2, 2}, Move{6, 6})
	g.Style = "alphago"
	result := g.SearchShard(0, 1, 2, g.SearchConfig().NodeBudget)
	if !result.Complete || result.Move != (Move{-1, -1}) {
		t.Fatalf("settled search should pass: %+v", result)
	}
	results := make([]SearchResult, 4)
	for shard := range results {
		results[shard] = g.SearchShard(shard, len(results), 2, g.SearchConfig().NodeBudget)
	}
	if move, err := g.MergeSearch(results); err != nil || move != (Move{-1, -1}) {
		t.Fatalf("empty shards must not lose the pass: %v, %v", move, err)
	}
	g.Resign()
	if move := g.ChooseMove(); move != (Move{-1, -1}) {
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
			g.Style = "alphago"
			result := g.SearchShard(0, 1, 3, g.SearchConfig().NodeBudget)
			if !result.Complete {
				t.Fatalf("fixed round incomplete: %+v", result)
			}
			play(t, g, result.Move)
		})
	}
}

func TestSearchShardsAreDisjointAndEquivalent(t *testing.T) {
	g := searchMidgame(t, 19)
	g.Style = "alphago"
	config := g.SearchConfig()
	serial := g.SearchShard(0, 1, 3, config.NodeBudget)
	if !serial.Complete || serial.TotalRoots != 64 {
		t.Fatalf("expected 64 roots: %+v", serial)
	}
	for _, workers := range []int{2, 8, 32, 64} {
		results := make([]SearchResult, workers)
		nodes, roots := 0, 0
		for shard := range results {
			results[shard] = g.SearchShard(shard, workers, 3, config.NodeBudget)
			nodes += results[shard].Nodes
			roots += results[shard].Roots
		}
		move, err := g.MergeSearch(results)
		if err != nil || move != serial.Move || nodes != serial.Nodes || roots != serial.TotalRoots {
			t.Fatalf("%d workers differ from serial: move=%v nodes=%d roots=%d err=%v", workers, move, nodes, roots, err)
		}
	}
}

func TestSearchMergeRejectsIncomparableRounds(t *testing.T) {
	g := snapbackGame(t)
	results := []SearchResult{g.SearchShard(0, 2, 2, 2048), g.SearchShard(1, 2, 2, 2048)}
	if _, err := g.MergeSearch(results); err != nil {
		t.Fatal(err)
	}
	for _, corrupt := range []func([]SearchResult){
		func(r []SearchResult) { r[1].Depth++ },
		func(r []SearchResult) { r[1].Complete = false },
		func(r []SearchResult) { r[1] = r[0] },
		func(r []SearchResult) { r[1].Roots++ },
	} {
		copy := append([]SearchResult(nil), results...)
		corrupt(copy)
		if _, err := g.MergeSearch(copy); err == nil {
			t.Fatal("invalid round was accepted")
		}
	}
}

func TestNativeSearchAdaptsAndStops(t *testing.T) {
	g := searchMidgame(t, 19)
	g.Style = "alphago"
	config := g.SearchConfig()
	config.ThinkMS = 25
	before := append([]int(nil), g.Board...)
	for _, workers := range []int{1, 4} {
		move, stats := g.searchNative(g.rankMoves(g.Style), config, workers, time.Now())
		if stats.Workers != workers || stats.Nodes == 0 || stats.Depth < 1 {
			t.Fatalf("no real search work: %+v", stats)
		}
		if stats.ElapsedMS > 1000 {
			t.Fatalf("deadline overrun: %+v", stats)
		}
		if _, _, err := g.tryMove(move); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, g.Board) {
			t.Fatal("parallel thinking mutated the board")
		}
		t.Logf("workers=%d depth=%d nodes=%d elapsed=%dms", workers, stats.Depth, stats.Nodes, stats.ElapsedMS)
	}
	start := time.Now()
	result := g.SearchShard(0, 1, 6, config.NodeBudget, 1)
	if result.Complete || time.Since(start) > time.Second {
		t.Fatalf("timed shard did not stop: %+v", result)
	}
}

func TestChooseMoveUsesAvailableProcessors(t *testing.T) {
	if runtime.GOARCH == "wasm" {
		t.Skip("browser parallelism uses independent Web Workers")
	}
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	g := snapbackGame(t)
	_, stats := g.ChooseMoveWithStats()
	if stats.Workers != 2 || stats.Depth < 1 || stats.Nodes < stats.Roots {
		t.Fatalf("native search did not use the available processors: %+v", stats)
	}
	g.Style = "china"
	move, stats := g.ChooseMoveWithStats()
	if stats != (SearchStats{}) || move != g.rankMoves(g.Style)[0].move {
		t.Fatal("the heuristic personality should keep its original behavior")
	}
}

func BenchmarkSearch19(b *testing.B) {
	g := searchMidgame(b, 19)
	g.Style = "alphago"
	for _, workers := range []int{1, 4, 8} {
		b.Run(strconv.Itoa(workers), func(b *testing.B) {
			b.ReportAllocs()
			var depths, nodes int64
			for i := 0; i < b.N; i++ {
				_, stats := g.searchNative(g.rankMoves(g.Style), g.SearchConfig(), workers, time.Now())
				depths += int64(stats.Depth)
				nodes += int64(stats.Nodes)
			}
			b.ReportMetric(float64(depths)/float64(b.N), "depth/op")
			b.ReportMetric(float64(nodes)/float64(b.N), "nodes/op")
		})
	}
}
