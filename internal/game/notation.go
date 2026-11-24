package game

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const columns = "ABCDEFGHJKLMNOPQRST"

func FormatMove(move Move, size int) string {
	if move == (Move{X: -1, Y: -1}) {
		return "pass"
	}
	if move.X < 0 || move.X >= size || move.Y < 0 || move.Y >= size || size > len(columns) {
		return "?"
	}
	return fmt.Sprintf("%c%d", columns[move.X], size-move.Y)
}

// ParseMove accepts coordinates and pass. Resignation belongs to the referee.
func ParseMove(value string, size int) (Move, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "PASS" {
		return Move{X: -1, Y: -1}, nil
	}
	if len(value) >= 2 && len(value) <= 3 {
		x := strings.IndexByte(columns, value[0])
		row, err := strconv.Atoi(value[1:])
		if err == nil && x >= 0 && x < size && row >= 1 && row <= size && value[1:] == strconv.Itoa(row) {
			return Move{X: x, Y: size - row}, nil
		}
	}
	return Move{}, errors.New("请使用 D4 这样的坐标（跳过 I 列）、pass 或 resign。")
}

func LegalMoves(g *Game) []string {
	legal := []string{}
	if g.Over {
		return legal
	}
	for y := 0; y < g.Size; y++ {
		for x := 0; x < g.Size; x++ {
			move := Move{X: x, Y: y}
			if g.IsLegal(move) {
				legal = append(legal, FormatMove(move, g.Size))
			}
		}
	}
	return append(legal, "pass", "resign")
}

func ASCII(g *Game) string {
	var board strings.Builder
	fmt.Fprint(&board, "   ")
	for x := 0; x < g.Size; x++ {
		fmt.Fprintf(&board, "%c ", columns[x])
	}
	fmt.Fprintln(&board)
	for y := 0; y < g.Size; y++ {
		fmt.Fprintf(&board, "%2d ", g.Size-y)
		for x := 0; x < g.Size; x++ {
			fmt.Fprintf(&board, "%c ", ".XO"[g.Board[y*g.Size+x]])
		}
		fmt.Fprintf(&board, "%d\n", g.Size-y)
	}
	fmt.Fprint(&board, "X = black, O = white, . = empty")
	return board.String()
}
