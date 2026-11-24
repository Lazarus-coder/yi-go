package game

import "testing"

func TestPolicyFeaturesFollowThePlayerAndBoardSymmetries(t *testing.T) {
	// C2 captures White's C3 stone. Its features must describe the same
	// local fight from either color and in all eight board orientations.
	sequence := []Move{{2, 5}, {2, 6}, {1, 6}, {6, 2}, {3, 6}, {6, 3}}
	var reference policyFeatures
	for _, first := range []int{Black, White} {
		for symmetry := 0; symmetry < 8; symmetry++ {
			g := newGame(t)
			g.Next = first
			transform := func(move Move) Move {
				if symmetry >= 4 {
					move.X = g.Size - 1 - move.X
				}
				for i := 0; i < symmetry%4; i++ {
					move = Move{g.Size - 1 - move.Y, move.X}
				}
				return move
			}
			for _, move := range sequence {
				play(t, g, transform(move))
			}
			capture := transform(Move{2, 7})
			var actual policyFeatures
			g.rankCandidates("alphago", func(move Move, features policyFeatures, heuristic float64) float64 {
				if move == capture {
					actual = features
				}
				return heuristic
			})
			if actual[12] != scaledCount(1) || actual[17] != 0.25 {
				t.Fatalf("expected a one-stone capture touching one enemy group, got %v", actual)
			}
			if first == Black && symmetry == 0 {
				reference = actual
			}
			if actual != reference {
				t.Fatalf("features changed for color %d, symmetry %d: %v vs %v", first, symmetry, actual, reference)
			}
		}
	}
}

func TestPolicyGuidesOpeningSearch(t *testing.T) {
	g := newGame(t)
	g.Style = "alphago"
	play(t, g, Move{4, 4}) // E5
	heuristic := g.rankCandidates(g.Style, func(_ Move, _ policyFeatures, score float64) float64 { return score })
	preferred := g.rankMoves(g.Style)[0].move
	// On the empty third line, the learned policy approaches E5 along an
	// axis. The old spacing heuristic instead favors a diagonal offset.
	if abs(preferred.X-4)+abs(preferred.Y-4) != 2 || (preferred.X != 4 && preferred.Y != 4) ||
		preferred == heuristic[0].move {
		t.Fatalf("policy approach %v, heuristic approach %v", preferred, heuristic[0].move)
	}
	for _, depth := range []int{1, 2} {
		result := g.SearchShard(0, 1, depth, g.SearchConfig().NodeBudget)
		if !result.Complete || result.Move != preferred {
			t.Fatalf("depth %d failed to follow the policy in a quiet opening: %+v", depth, result)
		}
	}
}

func TestPolicyKeepsOpenUncontestedRegions(t *testing.T) {
	g := newGame(t)
	play(t, g, Move{2, 2})
	play(t, g, Move{-1, -1})
	if len(g.rankMoves("alphago")) == 0 {
		t.Fatal("a lone stone cannot settle the entire board for the policy")
	}
}
