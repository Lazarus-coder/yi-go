// Package match handles one complete human/CPU turn for HTTP and WebAssembly.
package match

import (
	"errors"
	"fmt"

	"yi-go/internal/game"
)

var (
	ErrTurn = errors.New("当前不能落子，请开始新对局或悔棋")
	ErrCPU  = errors.New("CPU 暂时无法落子，请重试")
)

type Request struct {
	Size     int         `json:"size"`
	Moves    []game.Move `json:"moves"`
	Action   string      `json:"action"`
	X        *int        `json:"x,omitempty"`
	Y        *int        `json:"y,omitempty"`
	Resigned int         `json:"resigned,omitempty"`
	Style    string      `json:"style,omitempty"`
}

// Run always reconstructs a fresh game; rejected requests cannot alter a save.
func Run(input Request) (*game.Game, error) {
	g, err := restore(input)
	if err != nil {
		return nil, err
	}
	switch input.Action {
	case "sync":
		// Changing style preserves every recorded move and affects future turns.
	case "play", "pass", "resign":
		if g.Over || g.Next != game.Black {
			return nil, ErrTurn
		}
		if input.Action == "resign" {
			g.Resign()
			break
		}
		if len(g.Moves)+2 > 2000 {
			return nil, errors.New("本局已达 2000 手上限，请悔棋或开始新对局")
		}
		move := game.Move{X: -1, Y: -1}
		if input.Action == "play" {
			if input.X == nil || input.Y == nil {
				return nil, errors.New("请选择棋盘上的交叉点")
			}
			move = game.Move{X: *input.X, Y: *input.Y}
			if move.X < 0 || move.Y < 0 {
				return nil, errors.New("落子坐标超出棋盘")
			}
		}
		if err := g.Play(move); err != nil {
			return nil, err
		}
		if !g.Over {
			if err := g.Play(g.ChooseMove()); err != nil {
				return nil, fmt.Errorf("%w：%v", ErrCPU, err)
			}
		}
	default:
		return nil, errors.New("未知操作")
	}
	return g, nil
}

func restore(input Request) (*game.Game, error) {
	style, err := game.NormalizeStyle(input.Style)
	if err != nil {
		return nil, err
	}
	if len(input.Moves) > 2000 {
		return nil, errors.New("棋谱超过示范版的 2000 手上限")
	}
	g, err := game.New(input.Size)
	if err != nil {
		return nil, err
	}
	g.Style = style
	for i, move := range input.Moves {
		if err := g.Play(move); err != nil {
			return nil, fmt.Errorf("棋谱第 %d 手无效：%w", i+1, err)
		}
	}
	if !g.Over && g.Next != game.Black {
		return nil, errors.New("棋谱缺少 CPU 应手，请使用完整回合的记录")
	}
	if input.Resigned != 0 {
		if input.Resigned != game.Black || g.Next != game.Black || g.Over {
			return nil, errors.New("认输记录无效")
		}
		g.Resign()
	}
	return g, nil
}
