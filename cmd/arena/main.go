// Arena runs configured paired matches and verifies the one public report format.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Lazarus-coder/yi-go/internal/arena"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "arena:", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: arena try [-out directory] -- command [args...] | arena run -config config.json -out output | arena verify -report report.json [-config reviewed.json]")
	}
	switch args[0] {
	case "try":
		return tryAgent(ctx, args[1:])
	case "run":
		flags := flag.NewFlagSet("run", flag.ContinueOnError)
		configPath := flags.String("config", "", "private tournament configuration")
		out := flags.String("out", "arena-output", "new output directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *configPath == "" || flags.NArg() != 0 {
			return errors.New("run requires -config and optional -out")
		}
		input, err := os.Open(*configPath)
		if err != nil {
			return err
		}
		config, err := arena.ReadConfig(input)
		input.Close()
		if err != nil {
			return err
		}
		_, err = runTournament(ctx, config, *out)
		return err
	case "verify":
		flags := flag.NewFlagSet("verify", flag.ContinueOnError)
		path := flags.String("report", "", "public report.json to replay and verify")
		configPath := flags.String("config", "", "optional reviewed roster/rules manifest")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *path == "" || flags.NArg() != 0 {
			return errors.New("verify requires -report and optional -config")
		}
		input, err := os.Open(*path)
		if err != nil {
			return err
		}
		report, err := arena.ReadReport(input)
		input.Close()
		if err != nil {
			return err
		}
		var reviewed *arena.Config
		if *configPath != "" {
			input, err := os.Open(*configPath)
			if err != nil {
				return err
			}
			config, err := arena.ReadReviewedConfig(input)
			input.Close()
			if err != nil {
				return err
			}
			reviewed = &config
		}
		if err := arena.Verify(report, reviewed); err != nil {
			return err
		}
		fmt.Printf("Verified %d games, all snapshots, terminal scores and standings; manifest %s\n", len(report.Games), report.Provenance.ManifestSHA256)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runTournament(ctx context.Context, config arena.Config, out string) (*arena.Report, error) {
	if err := os.MkdirAll(out, 0755); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(out, ".arena.lock")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("output directory is already in use; choose a new directory: %w", err)
	}
	lock.Close()
	defer os.Remove(lockPath)
	if _, err := os.Stat(filepath.Join(out, "report.json")); err == nil {
		return nil, errors.New("report already exists; use a new output directory")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	records := filepath.Join(out, "games")
	if err := os.MkdirAll(records, 0755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(records)
	if err != nil {
		return nil, err
	}
	if len(entries) != 0 {
		return nil, errors.New("game records already exist; use a new output directory")
	}
	report, err := arena.Run(ctx, config, func(record arena.GameRecord) error {
		if err := writeRecord(filepath.Join(records, record.ID+".json"), record); err != nil {
			return err
		}
		fmt.Printf("%s  %s vs %s  %s  winner=%s\n", record.ID, record.Black, record.White, record.Result.Reason, record.Result.Winner)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := arena.Verify(report, &config); err != nil {
		return nil, fmt.Errorf("refusing to publish an invalid report: %w", err)
	}
	path := filepath.Join(out, "report.json")
	if err := writeReport(path, report); err != nil {
		return nil, err
	}
	fmt.Printf("Verified %d games. Report: %s\n", len(report.Games), path)
	return report, nil
}

func writeRecord(path string, record arena.GameRecord) error {
	return publishJSON(path, record, 0444)
}

func writeReport(path string, report *arena.Report) error {
	return publishJSON(path, report, 0644)
}

func publishJSON(path string, value any, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".arena-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(value); err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	if err := os.Chmod(file.Name(), mode); err != nil {
		return err
	}
	return os.Link(file.Name(), path) // Atomic publication that cannot overwrite an existing report.
}
