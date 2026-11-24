package game

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestOpponentCatalogue(t *testing.T) {
	list := Opponents()
	if len(list) != 12 {
		t.Fatalf("got %d opponents, want all 12 teaching personalities", len(list))
	}
	seen := map[string]bool{}
	groups := map[string]int{}
	for _, entry := range list {
		if seen[entry.ID] || entry.Name == "" || entry.Group == "" || entry.Description == "" {
			t.Fatalf("duplicate or incomplete opponent: %+v", entry)
		}
		seen[entry.ID] = true
		if entry.Group == "进阶搜索" {
			if entry.Search == nil || entry.Search.Roots < 32 || entry.Search.ThinkMS <= 0 {
				t.Fatalf("advanced opponent lacks shared search configuration: %+v", entry)
			}
		} else if entry.Search != nil {
			t.Fatalf("heuristic opponent unexpectedly searches: %+v", entry)
		}
		groups[entry.Group]++
		if id, err := NormalizeStyle(entry.ID); err != nil || id != entry.ID {
			t.Fatalf("listed opponent %q cannot be selected: %v", entry.ID, err)
		}
	}
	if groups["入门陪练"] != 4 || groups["经典棋风"] != 3 || groups["棋社棋友"] != 2 || groups["进阶搜索"] != 3 {
		t.Fatalf("unexpected catalogue groups: %v", groups)
	}
	data, err := json.Marshal(list[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 4 || fields["id"] != list[0].ID {
		t.Fatalf("catalogue JSON should only expose four UI fields: %s", data)
	}
	list[0].Name = "changed by caller"
	if Opponents()[0].Name == list[0].Name {
		t.Fatal("a caller changed the built-in opponent catalogue")
	}
}

func TestBeginnerPersonalitiesHaveRecognizableChoices(t *testing.T) {
	choices := map[Move]bool{}
	for _, id := range []string{"beginner-chaser", "beginner-timid", "beginner-edge", "beginner-scatter"} {
		t.Run(id, func(t *testing.T) {
			g := newGame(t)
			g.Style, g.Next = id, White
			place(g, White, Move{2, 2})
			place(g, Black, Move{4, 4})
			g.LastMove = &Move{4, 4}
			move := g.ChooseMove()
			choices[move] = true
			if move != g.ChooseMove() {
				t.Fatal("the personality should choose consistently in the same position")
			}
			switch id {
			case "beginner-chaser":
				if abs(move.X-4)+abs(move.Y-4) != 1 {
					t.Fatalf("chaser should cling to the last enemy move, chose %v", move)
				}
			case "beginner-timid":
				if abs(move.X-2)+abs(move.Y-2) != 1 {
					t.Fatalf("timid player should overprotect the safe friendly stone, chose %v", move)
				}
			case "beginner-edge":
				if min(move.X, move.Y, g.Size-1-move.X, g.Size-1-move.Y) != 0 {
					t.Fatalf("edge lover should play too low on the first line, chose %v", move)
				}
			case "beginner-scatter":
				if abs(move.X-2)+abs(move.Y-2) < 4 {
					t.Fatalf("scattershot player should wander from its own stone, chose %v", move)
				}
			}
			play(t, g, move)
		})
	}
	if len(choices) < 3 {
		t.Fatalf("newbie personalities collapsed into the same choices: %v", choices)
	}
}

func TestChaserCanForgetToSaveItsOwnGroup(t *testing.T) {
	g := newGame(t)
	g.Next = White
	place(g, White, Move{4, 4}, Move{4, 5})
	place(g, Black, Move{3, 4}, Move{4, 3}, Move{5, 4}, Move{3, 5}, Move{5, 5}, Move{1, 1})
	g.LastMove = &Move{1, 1}
	if move := g.ChooseMove(); move != (Move{4, 6}) {
		t.Fatalf("balanced player should save the two stones, chose %v", move)
	}
	g.Style = "beginner-chaser"
	move := g.ChooseMove()
	if move == (Move{4, 6}) || abs(move.X-1)+abs(move.Y-1) != 1 {
		t.Fatalf("chaser should make its characteristic mistake: chase instead of saving, chose %v", move)
	}
	play(t, g, move)
}

func TestEveryOpponentKeepsThinkingPureAndPlaysLegally(t *testing.T) {
	for _, entry := range Opponents() {
		t.Run(entry.ID, func(t *testing.T) {
			g := newGame(t)
			g.Style, g.Next = entry.ID, White
			place(g, White, Move{2, 2}, Move{3, 2})
			place(g, Black, Move{4, 4}, Move{4, 5}, Move{2, 3})
			g.LastMove = &Move{4, 5}
			before, _ := json.Marshal(g)
			move := g.ChooseMove()
			after, _ := json.Marshal(g)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("thinking changed the recorded position")
			}
			if move == (Move{-1, -1}) {
				t.Fatal("opponent passed in an open, playable position")
			}
			play(t, g, move)
		})
	}
}

func TestNewOpponentSelfPlaySettles(t *testing.T) {
	for _, entry := range Opponents() {
		if entry.Group == "经典棋风" {
			continue // The original three already have self-play tests at every size.
		}
		t.Run(entry.ID, func(t *testing.T) {
			g := newGame(t)
			g.Style = entry.ID
			for turn := 0; turn < g.Size*g.Size*3 && !g.Over; turn++ {
				play(t, g, g.ChooseMove())
			}
			if !g.Over {
				t.Fatal("self-play should settle instead of continuing indefinitely")
			}
		})
	}
}
