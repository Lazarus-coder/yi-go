package arena

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Lazarus-coder/yi-go/internal/game"
)

type pairing struct {
	id           string
	round        int
	opening      Opening
	black, white PlayerConfig
}

func schedule(c Config) []pairing {
	var matches []pairing
	for round := 1; round <= c.Rounds; round++ {
		for _, opening := range c.Openings {
			for i := range c.Players {
				for j := i + 1; j < len(c.Players); j++ {
					for _, swap := range []bool{false, true} {
						black, white := c.Players[i], c.Players[j]
						if swap {
							black, white = white, black
						}
						matches = append(matches, pairing{fmt.Sprintf("g-%06d", len(matches)+1), round, opening, black, white})
					}
				}
			}
		}
	}
	return matches
}

type builtinPlayer struct{ style string }

func (p builtinPlayer) Move(ctx context.Context, position *game.Game) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	position.Style = p.style
	move := position.ChooseMove()
	return Decision{Move: move}, ctx.Err()
}
func (builtinPlayer) Close() error { return nil }

func createPlayer(ctx context.Context, config PlayerConfig, size, color int, info GameInfo) (Player, error) {
	if config.Kind == "builtin" {
		return builtinPlayer{config.Style}, nil
	}
	return NewPlayer(ctx, config.Driver, size, color, info)
}

// Run emits a finalized immutable record only after both players are cleaned up.
// Host setup/cleanup failures and cancellation abort, without manufacturing a win.
func Run(ctx context.Context, config Config, onGame func(GameRecord) error) (*Report, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	report := newReport(config)
	for _, pair := range schedule(config) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := runGame(ctx, config, pair)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", pair.id, err)
		}
		if onGame != nil {
			if err := onGame(record); err != nil {
				return nil, err
			}
		}
		report.Games = append(report.Games, record)
	}
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	report.Standings = standings(report.Players, report.Games)
	return report, nil
}

func isolated(g *game.Game) *game.Game {
	copy := *g
	copy.Board = append([]int(nil), g.Board...)
	copy.Moves = append([]game.Move{}, g.Moves...)
	if g.LastMove != nil {
		move := *g.LastMove
		copy.LastMove = &move
	}
	if g.Score != nil {
		score := *g.Score
		copy.Score = &score
	}
	return &copy
}

func requestMove(ctx context.Context, player Player, g *game.Game, timeout time.Duration) (Decision, error) {
	turn, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type answer struct {
		decision Decision
		err      error
	}
	ready := make(chan answer, 1)
	position := isolated(g)
	go func() {
		decision, err := player.Move(turn, position)
		ready <- answer{decision, err}
	}()
	select {
	case <-turn.Done():
		if _, native := player.(builtinPlayer); native {
			<-ready // Join bounded native search before its work can affect the next game.
		}
		return Decision{}, turn.Err()
	case result := <-ready:
		if err := turn.Err(); err != nil {
			return Decision{}, err
		}
		return result.decision, result.err
	}
}

func runGame(ctx context.Context, config Config, pair pairing) (record GameRecord, err error) {
	start := time.Now()
	g, _ := game.New(config.BoardSize)
	record = GameRecord{ID: pair.id, OpeningID: pair.opening.ID, Round: pair.round,
		Black: pair.black.ID, White: pair.white.ID, Size: config.BoardSize, Komi: config.Komi,
		OpeningPlies: len(pair.opening.Moves), Frames: []Frame{frame(g)}}
	for _, move := range pair.opening.Moves {
		if err := g.Play(move); err != nil {
			return record, err
		}
		record.Frames = append(record.Frames, frame(g))
	}
	players := make([]Player, 2)
	defer func() {
		for _, player := range players {
			if player != nil {
				if closeErr := player.Close(); closeErr != nil {
					err = errors.Join(err, fmt.Errorf("player cleanup: %w", closeErr))
				}
			}
		}
		if err == nil {
			record.DurationMS = time.Since(start).Milliseconds()
			record.Moves = append([]game.Move{}, g.Moves...)
			record.Frames[len(record.Frames)-1] = frame(g)
			record.SHA256 = recordHash(record)
		}
	}()
	for index, participant := range []PlayerConfig{pair.black, pair.white} {
		info := GameInfo{ID: pair.id, BlackID: pair.black.ID, WhiteID: pair.white.ID,
			MaxMoves: config.MaxMoves, TurnTimeoutMS: config.TurnTimeoutMS, OpeningPlies: len(pair.opening.Moves)}
		players[index], err = createPlayer(ctx, participant, config.BoardSize, index+1, info)
		if err != nil {
			var startup *StartupError
			if errors.As(err, &startup) || ctx.Err() != nil {
				return record, err
			}
			record.Result = forfeited(g, record, index+1, refereeFault(err), "startup", nil)
			return record, nil
		}
	}
	for !g.Over {
		decision, moveErr := requestMove(ctx, players[g.Next-1], g, time.Duration(config.TurnTimeoutMS)*time.Millisecond)
		if ctx.Err() != nil {
			return record, ctx.Err()
		}
		if moveErr != nil {
			var startup *StartupError
			if errors.As(moveErr, &startup) {
				return record, moveErr
			}
			record.Result = forfeited(g, record, g.Next, refereeFault(moveErr), "turn", nil)
			break
		}
		if decision.Resign {
			g.Resign()
			g.Score = nil // Resignation has no numeric margin.
			record.Result = decided(record, "resign", 3-g.Resigned, nil)
			break
		}
		if moveErr := g.Play(decision.Move); moveErr != nil {
			record.Result = forfeited(g, record, g.Next, "illegal-move", "turn", &decision.Move)
			break
		}
		record.Frames = append(record.Frames, frame(g))
		if g.Over {
			record.Result = decided(record, "passes", g.Score.Winner, g.Score)
		} else if len(g.Moves) == config.MaxMoves {
			score := capScore(g)
			g.Over, g.Score = true, score
			record.Result = decided(record, "move-cap", score.Winner, score)
		}
	}
	return record, nil
}

func capScore(g *game.Game) *game.Score {
	copy := isolated(g)
	for !copy.Over {
		_ = copy.Play(game.Move{X: -1, Y: -1})
	}
	return copy.Score
}

func decided(record GameRecord, reason string, winner int, score *game.Score) Result {
	result := Result{Reason: reason, WinnerColor: winner, Score: score}
	if winner == game.Black {
		result.Winner = record.Black
	} else if winner == game.White {
		result.Winner = record.White
	}
	return result
}

func forfeited(g *game.Game, record GameRecord, side int, fault, phase string, attempt *game.Move) Result {
	g.Over, g.Score = true, nil
	result := decided(record, "forfeit", 3-side, nil)
	result.Fault, result.FaultSide, result.Phase, result.Attempt = fault, side, phase, attempt
	return result
}

func refereeFault(err error) string {
	code := FaultCode(err)
	if code != FaultTimeout && code != FaultProtocol {
		return FaultProcess
	}
	return code
}
