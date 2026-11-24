package match

import (
	"errors"
	"slices"
	"testing"

	"github.com/Lazarus-coder/yi-go/internal/game"
)

func openingRequest(style string) Request {
	x, y := 2, 2
	return Request{Size: 9, Style: style, Action: "play", X: &x, Y: &y}
}

func TestPrepareAndAuthoritativeReply(t *testing.T) {
	input := openingRequest("alphago")
	position, err := prepare(input)
	if err != nil || len(position.Moves) != 1 || position.Next != game.White {
		t.Fatalf("prepare should stop before the CPU reply: %v, %+v", err, position)
	}
	reply := game.Move{X: 3, Y: 3}
	finished, err := applyReply(position, reply, nil)
	if err != nil || len(finished.Moves) != 2 || finished.Next != game.Black || finished.Moves[1] != reply {
		t.Fatalf("selected reply was not applied: %v, %+v", err, finished)
	}
	if len(input.Moves) != 0 {
		t.Fatal("applying a worker reply mutated its saved request")
	}
	position, err = prepare(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyReply(position, game.Move{X: 2, Y: 2}, nil); !errors.Is(err, ErrCPU) {
		t.Fatalf("occupied worker reply must be rejected by the rules: %v", err)
	}
	input.Moves = []game.Move{{X: 0, Y: 0}}
	if _, err := prepare(input); err == nil {
		t.Fatal("prepare accepted an incomplete saved human/CPU turn")
	}
}

func TestAnalyzeTerminalSyncAndHeuristic(t *testing.T) {
	for _, input := range []Request{
		{Size: 9, Style: "alphago", Action: "sync"},
		{Size: 9, Style: "alphago", Action: "resign"},
		{Size: 9, Style: "alphago", Action: "pass", Moves: []game.Move{{X: 2, Y: 2}, {X: -1, Y: -1}}},
		openingRequest("china"),
	} {
		result, err := Analyze(AnalysisRequest{Request: input})
		if err != nil || result.Game == nil || result.Analysis != nil || result.Game.Search != nil {
			t.Fatalf("non-search action should return a normal game: %v, %+v", err, result)
		}
		if input.Action == "pass" && (!result.Game.Over || len(result.Game.Moves) != 3) {
			t.Fatal("second pass must finish without an additional CPU reply")
		}
	}
}

func TestAnalysisBoundsAndMerge(t *testing.T) {
	input := openingRequest("alphago")
	baseline, err := Analyze(AnalysisRequest{Request: input})
	if err != nil || baseline.Analysis == nil || !baseline.Analysis.Complete || baseline.Config == nil {
		t.Fatalf("missing complete one-worker baseline: %v, %+v", err, baseline)
	}
	for _, bad := range []AnalysisRequest{
		{Shard: -1}, {Shard: 2, Shards: 2}, {Shards: -1},
		{Shards: baseline.Config.Roots + 1}, {Depth: -1},
		{Depth: baseline.Config.MaxDepth + 1}, {NodeBudget: -1},
		{NodeBudget: baseline.Config.NodeBudget + 1},
		{ThinkMS: -1}, {ThinkMS: baseline.Config.ThinkMS + 1},
	} {
		bad.Request = input
		if _, err := Analyze(bad); err == nil {
			t.Errorf("accepted invalid search bounds: %+v", bad)
		}
	}
	analyses := make([]game.SearchResult, 2)
	for shard := range analyses {
		part, err := Analyze(AnalysisRequest{Request: input, Shard: shard, Shards: len(analyses), Depth: 1})
		if err != nil || part.Analysis == nil {
			t.Fatalf("shard %d: %v", shard, err)
		}
		analyses[shard] = *part.Analysis
	}
	merged, err := Merge(MergeRequest{Request: input, Analyses: analyses})
	if err != nil {
		t.Fatal(err)
	}
	one, err := Merge(MergeRequest{Request: input, Analyses: []game.SearchResult{*baseline.Analysis}})
	if err != nil || !slices.Equal(merged.Moves, one.Moves) || !slices.Equal(merged.Board, one.Board) {
		t.Fatalf("worker count changed the completed-depth result: %v", err)
	}
	if merged.Search == nil || merged.Search.Workers != 2 || merged.Search.Depth != 1 {
		t.Fatalf("missing actual accepted-round stats: %+v", merged.Search)
	}
	if _, err := Merge(MergeRequest{Request: input, Analyses: analyses[:1]}); err == nil {
		t.Fatal("accepted an incomplete search round")
	}
	for i := range analyses {
		analyses[i].Move = game.Move{X: 2, Y: 2}
	}
	if _, err := Merge(MergeRequest{Request: input, Analyses: analyses}); err == nil {
		t.Fatal("accepted a searched move on an occupied point")
	}
}

func TestSearchStatsOnlyForSearchedReplies(t *testing.T) {
	for _, input := range []Request{openingRequest("china"), {Size: 9, Style: "alphago", Action: "sync"}} {
		result, err := Run(input)
		if err != nil || result.Search != nil {
			t.Fatalf("heuristic and sync results must omit search stats: %v, %+v", err, result)
		}
	}
	result, err := Run(openingRequest("pro-territory"))
	if err != nil || result.Search == nil || result.Search.Workers < 1 || result.Search.Depth < 1 {
		t.Fatalf("native searched reply must expose completed work: %v, %+v", err, result)
	}
}
