package game

import "math"

// ChooseMove is a small heuristic opponent, not a search engine. It favors
// captures, saving groups in atari, and well-spaced stones on the third line.
// Ties are deterministic so that games and examples are easy to reproduce.
func (g *Game) ChooseMove() Move {
	best := Move{-1, -1}
	if g.Over {
		return best
	}

	regions := emptyRegions(g.Board, g.Size)
	bestValue := math.Inf(-1)
	for point, color := range g.Board {
		if color != Empty || g.isTrueEye(point) {
			continue
		}
		move := Move{point % g.Size, point / g.Size}
		board, captured, err := g.tryMove(move)
		if err != nil {
			continue
		}
		stones, liberties := groupAt(board, g.Size, point)
		saved, threatened := g.tactics(point, len(liberties))

		// Filling our own territory adds no area. Small enemy regions are
		// usually settled too; do not invade them without a tactical reason.
		territory := regions[point]
		settled := territory.owner == g.Next ||
			(territory.owner == opponent(g.Next) && territory.size <= 6)
		if settled && captured == 0 && saved == 0 {
			continue
		}
		// Avoid offering a group with only one liberty unless it captures.
		if len(liberties) == 1 && captured == 0 {
			continue
		}

		value := 100*float64(captured) + 70*float64(saved) +
			12*float64(threatened) + g.spacing(point)
		if len(liberties) == 1 {
			value -= 30 * float64(len(stones))
		}
		if value > bestValue {
			bestValue = value
			best = move
		}
	}
	return best
}

// tactics counts adjacent groups once, even if they touch the move twice.
func (g *Game) tactics(point, newLiberties int) (saved, threatened int) {
	seen := make([]bool, len(g.Board))
	for _, neighbor := range neighbors(point, g.Size) {
		if g.Board[neighbor] == Empty || seen[neighbor] {
			continue
		}
		stones, liberties := groupAt(g.Board, g.Size, neighbor)
		for _, stone := range stones {
			seen[stone] = true
		}
		if g.Board[neighbor] == g.Next && len(liberties) == 1 && newLiberties > 1 {
			saved += len(stones)
		}
		if g.Board[neighbor] == opponent(g.Next) && len(liberties) == 2 {
			threatened += len(stones)
		}
	}
	return saved, threatened
}

func (g *Game) spacing(point int) float64 {
	x, y := point%g.Size, point/g.Size
	edge := min(x, y, g.Size-1-x, g.Size-1-y)
	// Third-line stones balance territory and room to grow.
	value := -2 * math.Abs(float64(edge-2))
	nearestOwn, nearestEnemy := 2*g.Size, 2*g.Size
	for other, color := range g.Board {
		if color == Empty {
			continue
		}
		distance := abs(x-other%g.Size) + abs(y-other/g.Size)
		if color == g.Next {
			nearestOwn = min(nearestOwn, distance)
		} else {
			nearestEnemy = min(nearestEnemy, distance)
		}
	}
	if nearestOwn < 2*g.Size {
		value -= 1.8 * math.Abs(float64(nearestOwn-3))
	}
	if nearestEnemy < 2*g.Size {
		value -= 0.8 * math.Abs(float64(nearestEnemy-3))
	}
	// Prefer a corner's third-line point over drifting along the edge.
	cornerDistance := min(x, g.Size-1-x) + min(y, g.Size-1-y)
	value -= 0.08 * float64(cornerDistance)
	return value
}

// A true eye has friendly orthogonal neighbors and sufficiently protected
// diagonals. At an edge every diagonal must be friendly; inside, one may differ.
func (g *Game) isTrueEye(point int) bool {
	for _, neighbor := range neighbors(point, g.Size) {
		if g.Board[neighbor] != g.Next {
			return false
		}
	}
	x, y := point%g.Size, point/g.Size
	badDiagonals := 0
	for _, dx := range []int{-1, 1} {
		for _, dy := range []int{-1, 1} {
			nx, ny := x+dx, y+dy
			if nx >= 0 && nx < g.Size && ny >= 0 && ny < g.Size &&
				g.Board[ny*g.Size+nx] != g.Next {
				badDiagonals++
			}
		}
	}
	if x == 0 || y == 0 || x == g.Size-1 || y == g.Size-1 {
		return badDiagonals == 0
	}
	return badDiagonals <= 1
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
