package game

import (
	"math"
	"sort"
	"time"
)

type treeSearch struct {
	limit, nodes, width int
	seeds               []candidate
	deadline            time.Time
}

func (s *treeSearch) exhausted() bool {
	return s.nodes >= s.limit || (!s.deadline.IsZero() && !time.Now().Before(s.deadline))
}

func (s *treeSearch) child(g *Game, move Move) (*Game, bool) {
	if s.exhausted() {
		return nil, false
	}
	s.nodes++
	child := *g
	// Play appends to Moves; a shallow copy could overwrite a parent's spare
	// backing-array capacity. Search needs position history only for simple ko.
	child.Moves = nil
	if child.Play(move) != nil {
		return nil, false
	}
	return &child, true
}

func (s *treeSearch) negamax(g *Game, depth int, alpha, beta float64) (float64, bool) {
	if depth == 0 || g.Over {
		return evaluatePosition(g), true
	}
	if s.exhausted() {
		return 0, false
	}
	best, searched := math.Inf(-1), 0
	for _, option := range g.searchReplies(s.seeds) {
		if searched == s.width {
			break
		}
		if s.exhausted() {
			return 0, false
		}
		child, legal := s.child(g, option.move)
		if !legal {
			continue
		}
		searched++
		value, complete := s.negamax(child, depth-1, -beta, -alpha)
		if !complete {
			return 0, false
		}
		best = max(best, -value)
		alpha = max(alpha, best)
		if alpha >= beta {
			return best, true
		}
	}
	// Pass is always a legal response, including when the selective list is empty.
	child, ok := s.child(g, Move{-1, -1})
	if !ok {
		return 0, false
	}
	value, complete := s.negamax(child, depth-1, -beta, -alpha)
	return max(best, -value), complete
}

// Replies combine weak groups anywhere on the board, the last move's vicinity,
// and promising global alternatives. No full-board rankMoves call per node.
func (g *Game) searchReplies(seeds []candidate) []candidate {
	priority := make([]float64, len(g.Board))
	offered, urgent := make([]bool, len(g.Board)), make([]bool, len(g.Board))
	offer := func(point int, value float64) {
		if g.Board[point] == Empty {
			offered[point] = true
			priority[point] += value
		}
	}
	for index, seed := range seeds {
		offer(seed.move.Y*g.Size+seed.move.X, 1-float64(index)*0.02)
	}
	if g.LastMove != nil {
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				x, y := g.LastMove.X+dx, g.LastMove.Y+dy
				if x >= 0 && x < g.Size && y >= 0 && y < g.Size && abs(dx)+abs(dy) <= 2 {
					offer(y*g.Size+x, 3-0.2*float64(abs(dx)+abs(dy)))
				}
			}
		}
	}
	seen := make([]bool, len(g.Board))
	for point, color := range g.Board {
		if color == Empty || seen[point] {
			continue
		}
		stones, liberties := groupAt(g.Board, g.Size, point)
		for _, stone := range stones {
			seen[stone] = true
		}
		if len(liberties) > 2 {
			continue
		}
		weight := 8.0
		if len(liberties) == 1 {
			weight = 100
			if color == g.Next {
				weight = 70
			}
		}
		for _, liberty := range liberties {
			offer(liberty, weight*float64(len(stones)))
			urgent[liberty] = urgent[liberty] || len(liberties) == 1
		}
	}
	regions := emptyRegions(g.Board, g.Size)
	var candidates []candidate
	for point, include := range offered {
		if !include || g.isTrueEye(point) {
			continue
		}
		territory := regions[point]
		if !urgent[point] && settledForPlay(territory, g.Next, g.Style == "alphago") {
			continue
		}
		candidates = append(candidates, candidate{move: Move{point % g.Size, point / g.Size}, score: priority[point]})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	return candidates
}

// Evaluation combines material, vulnerable groups and small enclosed areas.
// Large open regions are not counted as territory just because one stone borders them.
func evaluatePosition(g *Game) float64 {
	if g.Over && g.Score != nil {
		value := 12 * (g.Score.Black - g.Score.White)
		if g.Next == White {
			return -value
		}
		return value
	}
	value := -12 * Komi
	seen := make([]bool, len(g.Board))
	for point, color := range g.Board {
		if color == Empty || seen[point] {
			continue
		}
		stones, liberties := groupAt(g.Board, g.Size, point)
		for _, stone := range stones {
			seen[stone] = true
		}
		worth := 12 * float64(len(stones))
		if len(liberties) == 1 {
			worth -= 10*float64(len(stones)) + 6
		} else if len(liberties) == 2 {
			worth -= 3*float64(len(stones)) + 3
		}
		if color == Black {
			value += worth
		} else {
			value -= worth
		}
	}
	for _, territory := range emptyRegions(g.Board, g.Size) {
		if territory.size > max(8, len(g.Board)/10) {
			continue
		}
		if territory.owner == Black {
			value += 8
		} else if territory.owner == White {
			value -= 8
		}
	}
	if g.Next == White {
		return -value
	}
	return value
}
