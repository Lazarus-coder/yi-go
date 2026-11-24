package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Lazarus-coder/yi-go/internal/arena"
	"github.com/Lazarus-coder/yi-go/internal/game"
)

func tryAgent(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("try", flag.ContinueOnError)
	out := flags.String("out", "", "save the two games and replay report here (default: fresh runs/try-* directory)")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: arena try [-out directory] -- command [args...]")
		fmt.Fprintln(flags.Output(), "Play your trusted local JSONL command in both colors against the neural opponent on 9×9.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() == 0 || flags.Arg(0) == "" {
		return errors.New("supply your local JSONL agent, e.g. arena try -- python3 my_agent.py")
	}
	config := arena.Config{
		SchemaVersion: arena.SchemaVersion, Title: "Local agent trial", BoardSize: 9, Komi: game.Komi,
		TurnTimeoutMS: 30000, MaxMoves: 144, Rounds: 1,
		Openings: []arena.Opening{{ID: "empty", Name: "Empty board"}},
		Players: []arena.PlayerConfig{
			{ID: "agent", Name: "Local agent", Kind: "external", Version: "local",
				Driver: arena.DriverConfig{Mode: "command", Command: flags.Args()}},
			{ID: "neural", Name: "Yi Neural", Kind: "builtin", Style: "alphago", Version: "neural-policy-v1"},
		},
	}
	if *out == "" {
		if err := os.MkdirAll("runs", 0755); err != nil {
			return err
		}
		dir, err := os.MkdirTemp("runs", "try-")
		if err != nil {
			return err
		}
		*out = dir
	}
	fmt.Println("Playing both colors against Yi Neural on 9×9 (144 plies per game).")
	report, err := runTournament(ctx, config, *out)
	if err != nil {
		return err
	}
	fmt.Println("Watch both games: open the report above at https://lazarus-coder.github.io/yi-go/?open=1")
	for _, record := range report.Games {
		if record.Result.Reason == "forfeit" {
			return fmt.Errorf("%s: %s at revision %d: %w", record.ID, record.Result.Phase, len(record.Moves), record.DecisionError)
		}
	}
	return nil
}
