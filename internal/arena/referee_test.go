package arena

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lazarus-coder/yi-go/internal/game"
)

func refereeConfig() Config {
	return Config{SchemaVersion: 1, Title: "Referee test", BoardSize: 9, Komi: 6.5,
		TurnTimeoutMS: 1000, MaxMoves: 8, Rounds: 1,
		Openings: []Opening{{ID: "empty", Name: "Empty board", Moves: []game.Move{}}},
		Players: []PlayerConfig{{ID: "steady", Name: "Steady", Kind: "builtin", Style: "china"},
			{ID: "fighter", Name: "Fighter", Kind: "builtin", Style: "korea"}}}
}

func TestRefereePairedScheduleCapAndReplay(t *testing.T) {
	config := refereeConfig()
	config.Rounds = 2
	config.Openings = append(config.Openings, Opening{ID: "corner", Name: "Corner", Moves: []game.Move{{X: 2, Y: 2}, {X: 6, Y: 6}}})
	report, err := Run(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Games) != 8 {
		t.Fatalf("paired schedule has %d games, want 8", len(report.Games))
	}
	for index, record := range report.Games {
		if record.Result.Reason != "move-cap" || len(record.Moves) != config.MaxMoves || record.Result.Score == nil {
			t.Fatalf("move-cap result missing: %+v", record.Result)
		}
		if index%2 == 1 && (record.Black != report.Games[index-1].White || record.White != report.Games[index-1].Black) {
			t.Fatal("paired colors were not exchanged")
		}
	}
	for _, row := range report.Standings {
		if row.Played != 8 || row.MoveCaps != 8 || row.Wins+row.Losses+row.Draws != row.Played {
			t.Fatalf("inconsistent ladder: %+v", row)
		}
	}
	if err := Verify(report, &config); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(report)
	decoded, err := ReadReport(bytes.NewReader(encoded))
	if err != nil || Verify(decoded, &config) != nil {
		t.Fatalf("serialized report does not verify: %v", err)
	}
}

func TestRefereeRejectsConfigurationBeforePlaying(t *testing.T) {
	for name, change := range map[string]func(*Config){
		"komi":             func(c *Config) { c.Komi = 7.5 },
		"roster":           func(c *Config) { c.Players[1].ID = c.Players[0].ID },
		"terminal opening": func(c *Config) { c.Openings[0].Moves = []game.Move{{X: -1, Y: -1}, {X: -1, Y: -1}} },
		"occupied opening": func(c *Config) { c.Openings[0].Moves = []game.Move{{X: 0, Y: 0}, {X: 0, Y: 0}} },
		"timeout":          func(c *Config) { c.TurnTimeoutMS = 0 },
		"driver":           func(c *Config) { c.Players[1].Kind, c.Players[1].Version, c.Players[1].Style = "external", "v1", "" },
	} {
		t.Run(name, func(t *testing.T) {
			config := refereeConfig()
			change(&config)
			called := false
			if _, err := Run(context.Background(), config, func(GameRecord) error { called = true; return nil }); err == nil || called {
				t.Fatal("invalid tournament was started")
			}
		})
	}
}

func cloneReport(t *testing.T, report *Report) *Report {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := ReadReport(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return copy
}

func TestVerifyRecomputesInsteadOfTrustingHashes(t *testing.T) {
	config := refereeConfig()
	report, err := Run(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Report){
		"score":      func(r *Report) { r.Games[0].Result.Score.White++ },
		"snapshot":   func(r *Report) { r.Games[0].Frames[1].Board = strings.Repeat("0", 81) },
		"replay":     func(r *Report) { r.Games[0].Moves[1] = r.Games[0].Moves[0] },
		"schedule":   func(r *Report) { r.Games[0].White = r.Games[0].Black },
		"standing":   func(r *Report) { r.Standings[0].Wins++ },
		"cap-policy": func(r *Report) { r.Config.MoveCapPolicy = "draw" },
		"late resignation": func(r *Report) {
			record := &r.Games[0]
			last := &record.Frames[len(record.Frames)-1]
			record.Result = decided(*record, "resign", 3-last.Next, nil)
			last.Resigned, last.Score = last.Next, nil
			r.Standings = standings(r.Players, r.Games)
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := cloneReport(t, report)
			change(copy)
			copy.Games[0].SHA256 = recordHash(copy.Games[0])
			copy.Provenance.ManifestSHA256 = manifestHash(copy.Config, copy.Players)
			if Verify(copy, nil) == nil {
				t.Fatal("tampered report passed verification")
			}
		})
	}
	config.Players[0].Name = "Different reviewed identity"
	if Verify(report, &config) == nil {
		t.Fatal("report was accepted against a different reviewed manifest")
	}
}

func helperConfig(t *testing.T, mode string) PlayerConfig {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return PlayerConfig{ID: "agent", Name: "Protocol fixture", Kind: "external", Version: "fixture-v1", Policy: mode,
		Driver: DriverConfig{Mode: "command", Command: []string{executable, "-test.run=^TestRefereeAgentHelper$", "--", "--arena-helper=" + mode}}}
}

func TestRefereeAgentFaultsAndResignation(t *testing.T) {
	for mode, reason := range map[string]string{"illegal": "forfeit", "timeout": "forfeit", "protocol": "forfeit", "crash": "forfeit", "resign": "resign"} {
		t.Run(mode, func(t *testing.T) {
			config := refereeConfig()
			config.TurnTimeoutMS = 3000
			if mode == "timeout" {
				config.TurnTimeoutMS = 80
			}
			config.Openings[0].Moves = []game.Move{{X: 0, Y: 0}}
			config.Players[0] = helperConfig(t, mode)
			report, err := Run(context.Background(), config, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, record := range report.Games {
				if record.Result.Reason != reason || record.Result.Winner != "fighter" || record.Result.Score != nil {
					t.Fatalf("fault did not lose the correct side: %+v", record.Result)
				}
				if reason == "forfeit" {
					want := map[string]string{"illegal": "illegal-move", "timeout": "timeout", "protocol": "protocol", "crash": "process"}[mode]
					if record.Result.Fault != want || record.Result.Phase != "turn" {
						t.Fatalf("wrong fault classification: %+v", record.Result)
					}
				}
			}
			if err := Verify(report, &config); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(report)
			if bytes.Contains(data, []byte(config.Players[0].Driver.Command[0])) || bytes.Contains(data, []byte("--arena-helper")) {
				t.Fatal("public provenance leaked local executable/arguments")
			}
		})
	}
}

func TestRefereePassesAndInfrastructureFailure(t *testing.T) {
	config := refereeConfig()
	config.Players[0], config.Players[1] = helperConfig(t, "pass"), helperConfig(t, "pass")
	config.Players[1].ID = "other"
	report, err := Run(context.Background(), config, nil)
	if err != nil || Verify(report, &config) != nil {
		t.Fatalf("passing game invalid: %v", err)
	}
	if err := Verify(report, nil); err != nil {
		t.Fatalf("public report required a local launch configuration: %v", err)
	}
	reviewed := config
	reviewed.Players = append([]PlayerConfig(nil), config.Players...)
	reviewed.Players[0].Driver.StartupTimeoutMS = 6000
	if Verify(report, &reviewed) == nil {
		t.Fatal("changed execution timeout was not bound to the manifest")
	}
	for _, record := range report.Games {
		if record.Result.Reason != "passes" || len(record.Moves) != 2 || record.Result.WinnerColor != game.White {
			t.Fatalf("incorrect consecutive-pass result: %+v", record.Result)
		}
	}
	config.Players[0].Driver.Command[0] = filepath.Join(t.TempDir(), "missing-agent")
	if report, err := Run(context.Background(), config, nil); err == nil || report != nil {
		t.Fatal("host startup failure must abort without a scored report")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if report, err := Run(ctx, refereeConfig(), nil); !errors.Is(err, context.Canceled) || report != nil {
		t.Fatal("cancellation must abort without a scored report")
	}
}

func TestExecutionMetadataPublishesEffectiveLimits(t *testing.T) {
	config := refereeConfig()
	config.Players[0] = PlayerConfig{ID: "sandbox", Name: "Sandbox", Kind: "external", Version: "v1",
		Driver: DriverConfig{Mode: "oci", Image: "private-registry/model:secret", Command: []string{"/private/agent", "--key=secret"}}}
	public, players := manifest(config)
	if players[0].Execution != (Execution{Mode: "oci", StartupTimeoutMS: 5000, Runtime: "docker", MemoryMB: 256, CPUs: 1}) {
		t.Fatalf("wrong effective OCI limits: %+v", players[0].Execution)
	}
	if err := verifyExecution(players[0]); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(players)
	if bytes.Contains(data, []byte("secret")) || bytes.Contains(data, []byte("/private")) {
		t.Fatal("launch configuration leaked into public execution metadata")
	}
	baseline := manifestHash(public, players)
	for _, change := range []func(*DriverConfig){
		func(d *DriverConfig) { d.CPUs = 2 },
		func(d *DriverConfig) { d.MemoryMB = 512 },
		func(d *DriverConfig) { d.Runtime = "podman" },
		func(d *DriverConfig) { d.StartupTimeoutMS = 6000 },
	} {
		changed := config
		changed.Players = append([]PlayerConfig(nil), config.Players...)
		change(&changed.Players[0].Driver)
		public, players := manifest(changed)
		if manifestHash(public, players) == baseline {
			t.Fatal("execution budget change did not change the reviewed manifest")
		}
	}
}

// The referee tests exercise the real process driver without a shell or fixture binary.
func TestRefereeAgentHelper(t *testing.T) {
	mode := ""
	for _, argument := range os.Args {
		if strings.HasPrefix(argument, "--arena-helper=") {
			mode = strings.TrimPrefix(argument, "--arena-helper=")
		}
	}
	if mode == "" {
		return
	}
	input := bufio.NewScanner(os.Stdin)
	input.Buffer(make([]byte, 1024), 128<<10)
	output := json.NewEncoder(os.Stdout)
	for input.Scan() {
		var message struct {
			Type, Protocol, ID string
			Revision           int
		}
		if json.Unmarshal(input.Bytes(), &message) != nil {
			os.Exit(2)
		}
		if message.Type == "hello" {
			_ = output.Encode(map[string]any{"type": "ready", "protocol": message.Protocol, "id": message.ID})
			continue
		}
		move := "pass"
		switch mode {
		case "illegal":
			move = "A9"
		case "resign":
			move = "resign"
		case "timeout":
			time.Sleep(5 * time.Second)
		case "protocol":
			_ = output.Encode(map[string]string{"bad": "message"})
			continue
		case "crash":
			os.Exit(3)
		}
		_ = output.Encode(map[string]any{"type": "move", "id": message.ID, "revision": message.Revision, "move": move})
	}
	os.Exit(0)
}
