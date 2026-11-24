package arena

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Lazarus-coder/yi-go/internal/game"
)

// Supply an actual engine argv explicitly; default tests need no external engine.
func TestExternalGTPReference(t *testing.T) {
	value := os.Getenv("YI_GTP_COMMAND")
	if value == "" {
		t.Skip("set YI_GTP_COMMAND to a JSON argv for a real GTP v2 engine")
	}
	var command []string
	if json.Unmarshal([]byte(value), &command) != nil || len(command) == 0 {
		t.Fatal("YI_GTP_COMMAND must be a nonempty JSON argv")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	bridge, _ := filepath.Abs("../../examples/gtp/agent.py")
	argv := append([]string{python, bridge, "--"}, command...)
	config := refereeConfig()
	config.Title, config.MaxMoves, config.TurnTimeoutMS = "GTP integration", 32, 5000
	config.Openings = []Opening{{ID: "preset-pass", Name: "Both colors and pass", Moves: []game.Move{{X: 2, Y: 6}, {X: 6, Y: 2}, {X: -1, Y: -1}, {X: 3, Y: 4}}}}
	config.Players = []PlayerConfig{
		{ID: "engine-a", Name: "External GTP A", Kind: "external", Version: "local-integration", Driver: DriverConfig{Mode: "command", Command: argv}},
		{ID: "engine-b", Name: "External GTP B", Kind: "external", Version: "local-integration", Driver: DriverConfig{Mode: "command", Command: argv}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	report, err := Run(ctx, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Games) != 2 {
		t.Fatalf("expected color-swapped paired games, got %d", len(report.Games))
	}
	for _, record := range report.Games {
		if record.Result.Reason == "forfeit" || len(record.Moves) <= len(config.Openings[0].Moves) {
			t.Fatalf("real GTP bridge failed: %+v", record.Result)
		}
		t.Logf("%s: %d legal plies, %s", record.ID, len(record.Moves), record.Result.Reason)
	}
	if err := Verify(report, &config); err != nil {
		t.Fatal(err)
	}
}
