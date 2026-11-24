package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lazarus-coder/yi-go/internal/arena"
)

func TestTryAgent(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("example JSONL agent needs Python 3")
	}
	t.Chdir(t.TempDir())
	const seat = `import json,sys
for line in sys.stdin:
    r = json.loads(line)
    if r["type"] == "hello":
        reply = {"type":"ready", "id":r["id"], "protocol":r["protocol"]}
    else:
        move = r["legal"][0] if r["revision"] < 2 else "resign"
        reply = {"type":"move", "id":r["id"], "revision":r["revision"], "move":move}
    print(json.dumps(reply), flush=True)`
	ctx := context.Background()
	if err := execute(ctx, []string{"try", "--", python, "-c", seat}); err != nil {
		t.Fatal(err)
	}
	reports, err := filepath.Glob("runs/try-*/report.json")
	if err != nil || len(reports) != 1 {
		t.Fatalf("expected one new trial, got %v: %v", reports, err)
	}
	file, err := os.Open(reports[0])
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	report, err := arena.ReadReport(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.Verify(report, nil); err != nil {
		t.Fatal(err)
	}
	if len(report.Games) != 2 || report.Games[0].Black != "agent" || report.Games[1].White != "agent" ||
		report.Players[1].Style != "alphago" {
		t.Fatalf("wrong trial schedule: %+v", report.Config)
	}
	for _, record := range report.Games {
		if record.Result.Reason != "resign" || len(record.Moves) < 2 {
			t.Fatalf("real seat failed to play: %s %+v", record.ID, record.Result)
		}
	}

	brokenSeat := strings.Replace(seat, `"id":r["id"]`, `"id":"stale"`, 1)
	err = execute(ctx, []string{"try", "-out", "broken", "--", python, "-c", brokenSeat})
	if err == nil || !strings.Contains(err.Error(), "startup at revision 0") || !strings.Contains(err.Error(), "ready must match") {
		t.Fatalf("bad handshake needs an actionable error, got %v", err)
	}
	faultReport := filepath.Join("broken", "report.json")
	if err := execute(ctx, []string{"verify", "-report", faultReport}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(faultReport)
	if err != nil || strings.Contains(string(data), "ready must match") || strings.Contains(string(data), "stale") {
		t.Fatalf("published private diagnostic or command: %v", err)
	}
}

func TestTryNeedsCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := execute(context.Background(), []string{"try", "--"}); err == nil {
		t.Fatal("empty agent command succeeded")
	}
	if _, err := os.Stat("runs"); !os.IsNotExist(err) {
		t.Fatal("empty command created a run")
	}
}
