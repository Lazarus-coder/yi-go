package match

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Lazarus-coder/yi-go/internal/game"
)

// ExhibitionRequest carries only the public move record. Credentials belong to
// the private player transport and never enter the rules engine.
type ExhibitionRequest struct {
	History   []string `json:"history"`
	MaxMoves  int      `json:"maxMoves"`
	ChooseCPU bool     `json:"chooseCPU"`
}

type Exhibition struct {
	Game   *game.Game        `json:"game"`
	Turn   map[string]any    `json:"turn"`
	Result *ExhibitionResult `json:"result"`
	Choice string            `json:"choice,omitempty"`
}

type ExhibitionResult struct {
	Reason string      `json:"reason"`
	Winner string      `json:"winner"`
	Score  *game.Score `json:"score,omitempty"`
}

func InspectExhibition(input ExhibitionRequest) (*Exhibition, error) {
	if input.MaxMoves < 2 || input.MaxMoves > 144 || len(input.History) > input.MaxMoves {
		return nil, errors.New("对局上限须在 2 至 144 手之间，棋谱不能超过上限。")
	}
	g, _ := game.New(9)
	g.Style = "club-fighter"
	var result *ExhibitionResult
	for i, choice := range input.History {
		if g.Over {
			return nil, errors.New("本局已结束")
		}
		if strings.EqualFold(strings.TrimSpace(choice), "resign") {
			g.Resign()
			result = &ExhibitionResult{Reason: "resign", Winner: game.ColorName(g.Score.Winner)}
			g.Score = nil
			continue
		}
		move, err := game.ParseMove(choice, g.Size)
		if err == nil {
			err = g.Play(move)
		}
		if err != nil {
			return nil, fmt.Errorf("第 %d 手无效：%w", i+1, err)
		}
		switch {
		case g.Over:
			result = &ExhibitionResult{Reason: "passes", Winner: game.ColorName(g.Score.Winner), Score: g.Score}
		case len(g.Moves) == input.MaxMoves:
			g.Score, g.Over = g.AreaScore(), true
			result = &ExhibitionResult{Reason: "move-cap", Winner: game.ColorName(g.Score.Winner), Score: g.Score}
		}
	}
	state := &Exhibition{Game: g, Turn: game.Observe(g, input.MaxMoves), Result: result}
	if input.ChooseCPU {
		if g.Over {
			return nil, errors.New("本局已结束")
		}
		state.Choice = game.FormatMove(g.ChooseMove(), g.Size)
	}
	return state, nil
}
