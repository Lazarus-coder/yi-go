package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lazarus-coder/yi-go/internal/arena"
)

func TestConcurrentRunsCannotOverwriteOutput(t *testing.T) {
	directory := t.TempDir()
	config := arena.Config{SchemaVersion: 1, Title: "CLI test", BoardSize: 9, Komi: 6.5,
		TurnTimeoutMS: 1000, MaxMoves: 2, Rounds: 1,
		Openings: []arena.Opening{{ID: "empty", Name: "Empty"}},
		Players: []arena.PlayerConfig{{ID: "a", Name: "A", Kind: "builtin", Style: "china"},
			{ID: "b", Name: "B", Kind: "builtin", Style: "korea"}}}
	configPath := filepath.Join(directory, "config.json")
	data, _ := json.Marshal(config)
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(directory, "results")
	start, result := make(chan struct{}), make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			result <- execute(context.Background(), []string{"run", "-config", configPath, "-out", out})
		}()
	}
	close(start)
	first, second := <-result, <-result
	if (first == nil) == (second == nil) {
		t.Fatalf("expected exactly one successful writer: %v / %v", first, second)
	}
	reportPath := filepath.Join(out, "report.json")
	if err := execute(context.Background(), []string{"verify", "-report", reportPath, "-config", configPath}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(reportPath)
	if err := writeReport(reportPath, &arena.Report{}); err == nil {
		t.Fatal("an existing public report was overwritten")
	}
	after, _ := os.ReadFile(reportPath)
	if string(before) != string(after) {
		t.Fatal("failed publication changed the report")
	}
	if _, err := os.Stat(filepath.Join(out, ".arena.lock")); !os.IsNotExist(err) {
		t.Fatal("completed run left its lock behind")
	}
}
