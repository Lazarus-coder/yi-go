package game

import (
	"cmp"
	"math"
	"runtime"
	"slices"
	"time"
)

type candidate struct {
	move  Move
	score float64
}

// ChooseMove preserves weak personalities; advanced opponents adapt search depth
// to the machine. Scores at any completed depth remain deterministic.
func (g *Game) ChooseMove() Move {
	move, _ := g.ChooseMoveWithStats()
	return move
}

func (g *Game) ChooseMoveWithStats() (Move, SearchStats) {
	start := time.Now()
	ranked := g.rankMoves(g.Style)
	if len(ranked) == 0 {
		return Move{-1, -1}, SearchStats{}
	}
	if config := g.SearchConfig(); config.Roots > 0 {
		return g.searchNative(ranked, config, runtime.GOMAXPROCS(0), start)
	}
	return ranked[0].move, SearchStats{}
}

// rankMoves never searches: it can also rank an opponent's possible replies.
func (g *Game) rankMoves(id string) []candidate {
	if g.Over {
		return nil
	}
	style := profileFor(id)
	regions := emptyRegions(g.Board, g.Size)
	ranked := make([]candidate, 0, len(g.Board))
	var seed uint32
	if style.scatter != 0 {
		seed = uint32(g.Next)
		for point, color := range g.Board {
			seed = seed*16777619 ^ uint32(point*3+color)
		}
	}
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

		value := style.capture*float64(captured) + style.save*float64(saved) +
			style.threat*float64(threatened) + g.spacing(point, style) +
			style.stability*float64(min(len(liberties), 4))
		if style.chase != 0 && g.LastMove != nil && g.LastMove.X >= 0 {
			distance := abs(move.X-g.LastMove.X) + abs(move.Y-g.LastMove.Y)
			value -= style.chase * float64(distance)
		}
		if style.scatter != 0 {
			// Position-seeded distraction, not random mistakes that change on replay.
			noise := seed ^ uint32(point+1)*2654435761
			noise ^= noise >> 16
			noise *= 2246822519
			noise ^= noise >> 13
			value += style.scatter * float64(noise%1000) / 1000
		}
		if len(liberties) == 1 {
			value -= 30 * float64(len(stones))
		}
		ranked = append(ranked, candidate{move, value})
	}
	slices.SortStableFunc(ranked, func(a, b candidate) int { return cmp.Compare(b.score, a.score) })
	return ranked
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

func (g *Game) spacing(point int, style cpuStyle) float64 {
	x, y := point%g.Size, point/g.Size
	edge := min(x, y, g.Size-1-x, g.Size-1-y)
	value := -style.lineWeight * math.Abs(float64(edge-style.line))
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
		value -= style.ownWeight * math.Abs(float64(nearestOwn-style.ownDistance))
	}
	if nearestEnemy < 2*g.Size {
		value -= style.enemyWeight * math.Abs(float64(nearestEnemy-style.enemyDistance))
	}
	cornerDistance := min(x, g.Size-1-x) + min(y, g.Size-1-y)
	value -= style.cornerWeight * float64(cornerDistance)
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
