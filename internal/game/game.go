// Package game contains the rules and a small, deliberately readable Go opponent.
package game

import (
	"errors"
	"math"
	"slices"
)

const (
	Empty = 0
	Black = 1
	White = 2
	Komi  = 6.5
)

// Move uses zero-based board coordinates. {-1, -1} means pass.
type Move struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type Captures struct {
	Black int `json:"black"`
	White int `json:"white"`
}

type Score struct {
	Black  float64 `json:"black"`
	White  float64 `json:"white"`
	Winner int     `json:"winner"`
	Margin float64 `json:"margin"`
}

type Game struct {
	Size     int      `json:"size"`
	Style    string   `json:"style"`
	Board    []int    `json:"board"`
	Next     int      `json:"next"`
	Moves    []Move   `json:"moves"`
	LastMove *Move    `json:"lastMove"`
	Captures Captures `json:"captures"`
	Passes   int      `json:"passes"`
	Over     bool     `json:"over"`
	Resigned int      `json:"resigned"`
	Score    *Score   `json:"score"`

	// Simple ko forbids recreating the position before the previous move.
	koBoard []int
}

func New(size int) (*Game, error) {
	if size != 9 && size != 13 && size != 19 {
		return nil, errors.New("棋盘尺寸须为 9、13 或 19")
	}
	return &Game{
		Size:  size,
		Style: "china",
		Board: make([]int, size*size),
		Next:  Black,
		Moves: []Move{},
	}, nil
}

// NormalizeStyle keeps older saved games, which have no style, compatible.
func NormalizeStyle(value string) (string, error) {
	switch value {
	case "", "china":
		return "china", nil
	case "japan", "korea":
		return value, nil
	default:
		return "", errors.New("请选择中国、日本或韩国棋风")
	}
}

// Play applies one legal move. An invalid move leaves the game unchanged.
func (g *Game) Play(move Move) error {
	if g.Over {
		return errors.New("本局已结束")
	}
	if move.X == -1 && move.Y == -1 {
		g.Passes++
		g.koBoard = nil
		g.LastMove = nil
		g.Moves = append(g.Moves, move)
		g.Next = opponent(g.Next)
		if g.Passes == 2 {
			g.Over = true
			g.Score = g.areaScore()
		}
		return nil
	}

	board, captured, err := g.tryMove(move)
	if err != nil {
		return err
	}
	g.koBoard = g.Board
	g.Board = board
	if g.Next == Black {
		g.Captures.Black += captured
	} else {
		g.Captures.White += captured
	}
	g.Passes = 0
	g.Moves = append(g.Moves, move)
	g.LastMove = &move
	g.Next = opponent(g.Next)
	return nil
}

func (g *Game) Resign() {
	if g.Over {
		return
	}
	g.Over = true
	g.Resigned = g.Next
	g.Score = g.areaScore()
	g.Score.Winner = opponent(g.Next)
	g.Score.Margin = 0
}

// tryMove works on a copy, which also lets the CPU inspect candidates safely.
func (g *Game) tryMove(move Move) ([]int, int, error) {
	if move.X < 0 || move.X >= g.Size || move.Y < 0 || move.Y >= g.Size {
		return nil, 0, errors.New("请落在棋盘内")
	}
	point := move.Y*g.Size + move.X
	if g.Board[point] != Empty {
		return nil, 0, errors.New("此处已有棋子")
	}

	board := slices.Clone(g.Board)
	board[point] = g.Next
	captured := 0
	for _, neighbor := range neighbors(point, g.Size) {
		if board[neighbor] != opponent(g.Next) {
			continue
		}
		stones, liberties := groupAt(board, g.Size, neighbor)
		if len(liberties) == 0 {
			captured += len(stones)
			for _, stone := range stones {
				board[stone] = Empty
			}
		}
	}
	_, liberties := groupAt(board, g.Size, point)
	if len(liberties) == 0 {
		return nil, 0, errors.New("此处无气，不能落子")
	}
	if slices.Equal(board, g.koBoard) {
		return nil, 0, errors.New("打劫：请先在别处落子")
	}
	return board, captured, nil
}

// areaScore counts stones and empty regions bordered by just one color.
// There is no dead-stone adjudication: capture dead stones before passing.
func (g *Game) areaScore() *Score {
	score := &Score{White: Komi}
	regions := emptyRegions(g.Board, g.Size)
	for point, color := range g.Board {
		if color == Empty {
			color = regions[point].owner
		}
		switch color {
		case Black:
			score.Black++
		case White:
			score.White++
		}
	}
	if score.Black > score.White {
		score.Winner = Black
	} else if score.White > score.Black {
		score.Winner = White
	}
	score.Margin = math.Abs(score.Black - score.White)
	return score
}

type region struct {
	owner int
	size  int
}

// An empty region touching both colors (or neither) is neutral.
func emptyRegions(board []int, size int) []region {
	regions := make([]region, len(board))
	seen := make([]bool, len(board))
	for start, color := range board {
		if color != Empty || seen[start] {
			continue
		}
		points := []int{start}
		seen[start] = true
		borders := [3]bool{}
		for i := 0; i < len(points); i++ {
			for _, neighbor := range neighbors(points[i], size) {
				if board[neighbor] != Empty {
					borders[board[neighbor]] = true
				} else if !seen[neighbor] {
					seen[neighbor] = true
					points = append(points, neighbor)
				}
			}
		}
		owner := Empty
		if borders[Black] && !borders[White] {
			owner = Black
		} else if borders[White] && !borders[Black] {
			owner = White
		}
		for _, point := range points {
			regions[point] = region{owner: owner, size: len(points)}
		}
	}
	return regions
}

func groupAt(board []int, size, start int) (stones, liberties []int) {
	color := board[start]
	seen := make([]bool, len(board))
	seenLiberty := make([]bool, len(board))
	stones = []int{start}
	seen[start] = true
	for i := 0; i < len(stones); i++ {
		for _, neighbor := range neighbors(stones[i], size) {
			if board[neighbor] == Empty && !seenLiberty[neighbor] {
				seenLiberty[neighbor] = true
				liberties = append(liberties, neighbor)
			} else if board[neighbor] == color && !seen[neighbor] {
				seen[neighbor] = true
				stones = append(stones, neighbor)
			}
		}
	}
	return stones, liberties
}

func neighbors(point, size int) []int {
	result := make([]int, 0, 4)
	x, y := point%size, point/size
	if x > 0 {
		result = append(result, point-1)
	}
	if x < size-1 {
		result = append(result, point+1)
	}
	if y > 0 {
		result = append(result, point-size)
	}
	if y < size-1 {
		result = append(result, point+size)
	}
	return result
}

func opponent(color int) int { return 3 - color }
