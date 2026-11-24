package game

import "slices"

// The same color-relative features feed training and play. Reflections and
// rotations change neither the distances nor the counts on each local ring.
type policyFeatures [34]float32

type policyOffset struct {
	x, y, ring int
	weight     float32
}

var policyOffsets = func() []policyOffset {
	rings := [][2]int{{1, 0}, {1, 1}, {2, 0}, {2, 1}, {2, 2}, {3, 0}, {3, 1}, {4, 0}}
	var offsets []policyOffset
	for ring, radius := range rings {
		weight := float32(0.125)
		if radius[1] == 0 || radius[0] == radius[1] {
			weight = 0.25
		}
		for y := -radius[0]; y <= radius[0]; y++ {
			for x := -radius[0]; x <= radius[0]; x++ {
				if max(abs(x), abs(y)) == radius[0] && min(abs(x), abs(y)) == radius[1] {
					offsets = append(offsets, policyOffset{x, y, ring, weight})
				}
			}
		}
	}
	return offsets
}()

type policyBoard struct {
	chain    [maxPoints]int
	distance [3][maxPoints]int
	density  float32
}

func (p *policyBoard) prepare(g *Game) {
	occupied := 0
	for point, stone := range g.Board {
		if stone == Empty {
			continue
		}
		occupied++
		if p.chain[point] == 0 {
			stones, _ := groupAt(g.Board, g.Size, point)
			for _, member := range stones {
				p.chain[member] = point + 1
			}
		}
	}
	p.density = float32(occupied) / float32(len(g.Board))
	// A two-pass Manhattan distance transform costs two board scans per color.
	for color := Black; color <= White; color++ {
		distance := &p.distance[color]
		for point, stone := range g.Board {
			if stone == color {
				continue
			}
			distance[point] = 8
			if point%g.Size != 0 {
				distance[point] = min(distance[point], distance[point-1]+1)
			}
			if point >= g.Size {
				distance[point] = min(distance[point], distance[point-g.Size]+1)
			}
		}
		for point := len(g.Board) - 1; point >= 0; point-- {
			if point%g.Size != g.Size-1 {
				distance[point] = min(distance[point], distance[point+1]+1)
			}
			if point+g.Size < len(g.Board) {
				distance[point] = min(distance[point], distance[point+g.Size]+1)
			}
		}
	}
}

func scaledCount(v int) float32 { return float32(min(v, 8)) / 8 }

func (p *policyBoard) features(g *Game, point, liberties, stones, captured, saved, threatened int) policyFeatures {
	x, y := point%g.Size, point/g.Size
	ex, ey := min(x, g.Size-1-x), min(y, g.Size-1-y)
	edge := min(ex, ey)
	last := 8
	if g.LastMove != nil {
		last = abs(x-g.LastMove.X) + abs(y-g.LastMove.Y)
	}
	// Slots 0–7 describe the board and edges; 8–10 measure distance to play.
	// Slots 11–17 describe tactics and adjacent chains. The remaining pairs
	// count friendly and enemy stones in each of the eight local patterns.
	f := policyFeatures{
		0: p.density, 1: float32(g.Size-9) / 10, 2: float32(edge) / 9, 3: float32(max(ex, ey)) / 9,
		8: scaledCount(p.distance[g.Next][point]), 9: scaledCount(p.distance[opponent(g.Next)][point]),
		10: scaledCount(last), 11: scaledCount(liberties), 12: scaledCount(captured), 13: scaledCount(saved),
		14: scaledCount(threatened), 15: scaledCount(stones),
	}
	if edge < 4 {
		f[4+edge] = 1
	}
	var touched [4]int
	n := 0
	for _, neighbor := range neighbors(point, g.Size) {
		id := p.chain[neighbor]
		if id == 0 {
			continue
		}
		if !slices.Contains(touched[:n], id) {
			touched[n] = id
			n++
			if g.Board[neighbor] == g.Next {
				f[16] += 0.25
			} else {
				f[17] += 0.25
			}
		}
	}
	for _, offset := range policyOffsets {
		nx, ny := x+offset.x, y+offset.y
		if nx < 0 || ny < 0 || nx >= g.Size || ny >= g.Size {
			continue
		}
		switch g.Board[ny*g.Size+nx] {
		case Empty:
		case g.Next:
			f[18+2*offset.ring] += offset.weight
		default:
			f[19+2*offset.ring] += offset.weight
		}
	}
	return f
}
