package arena

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/Lazarus-coder/yi-go/internal/game"
)

func ReadReport(reader io.Reader) (*Report, error) {
	var report Report
	decoder := json.NewDecoder(io.LimitReader(reader, 128<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("report must contain one JSON object")
	}
	return &report, nil
}

func reportConfig(report *Report) Config {
	c := report.Config
	config := Config{SchemaVersion: report.SchemaVersion, Title: c.Title, BoardSize: c.BoardSize,
		Komi: c.Komi, TurnTimeoutMS: c.TurnTimeoutMS, MaxMoves: c.MaxMoves, Rounds: c.Rounds, Openings: c.Openings}
	for _, p := range report.Players {
		config.Players = append(config.Players, PlayerConfig{ID: p.ID, Name: p.Name, Kind: p.Kind, Style: p.Style, Version: p.Version, Policy: p.Policy})
	}
	return config
}

// Verify replays every move, recomputes every snapshot/result/standing and checks
// the complete paired schedule. It does not prove an external model's identity.
func Verify(report *Report, reviewed *Config) error {
	if report == nil || report.SchemaVersion != SchemaVersion || !report.Config.PairedColors || report.Config.MoveCapPolicy != "area-score" {
		return errors.New("unsupported report schema or scoring policy")
	}
	if _, err := time.Parse(time.RFC3339Nano, report.GeneratedAt); err != nil {
		return errors.New("invalid report timestamp")
	}
	config := reportConfig(report)
	if err := config.validate(false); err != nil {
		return err
	}
	for _, player := range report.Players {
		if err := verifyExecution(player); err != nil {
			return fmt.Errorf("player %s: %w", player.ID, err)
		}
	}
	if report.Provenance.EngineVersion != EngineVersion || report.Provenance.GoVersion == "" ||
		report.Provenance.ManifestSHA256 != manifestHash(report.Config, report.Players) {
		return errors.New("report manifest/provenance mismatch")
	}
	if reviewed != nil {
		if err := reviewed.validate(false); err != nil {
			return err
		}
		public, players := manifest(*reviewed)
		if manifestHash(public, players) != report.Provenance.ManifestSHA256 {
			return errors.New("report does not match the reviewed roster/rules/openings")
		}
	}
	pairs := schedule(config)
	if len(pairs) != len(report.Games) {
		return errors.New("report does not contain every scheduled paired game")
	}
	for index, pair := range pairs {
		if err := verifyGame(report.Games[index], pair, config); err != nil {
			return fmt.Errorf("%s: %w", pair.id, err)
		}
	}
	if !reflect.DeepEqual(report.Standings, standings(report.Players, report.Games)) {
		return errors.New("standings differ from the verified game results")
	}
	return nil
}

func verifyExecution(player Identity) error {
	e := player.Execution
	if player.Kind == "builtin" {
		if e != (Execution{Mode: "builtin"}) {
			return errors.New("builtin execution metadata is inconsistent")
		}
		return nil
	}
	if e.StartupTimeoutMS <= 0 || (e.Mode != "command" && e.Mode != "oci") {
		return errors.New("external execution metadata needs effective startup/resource limits")
	}
	driver := DriverConfig{Mode: e.Mode, StartupTimeoutMS: e.StartupTimeoutMS, Runtime: e.Runtime, MemoryMB: e.MemoryMB, CPUs: e.CPUs}
	if e.Mode == "command" {
		driver.Command = []string{"manifest-only"}
	} else {
		if e.MemoryMB <= 0 || e.CPUs <= 0 || e.Runtime == "" {
			return errors.New("OCI metadata must contain effective memory/CPU/runtime values")
		}
		driver.Image = "manifest-only"
	}
	return ValidateDriver(driver)
}

func verifyGame(record GameRecord, pair pairing, config Config) error {
	if record.ID != pair.id || record.OpeningID != pair.opening.ID || record.Round != pair.round ||
		record.Black != pair.black.ID || record.White != pair.white.ID || record.Size != config.BoardSize ||
		record.Komi != config.Komi || record.OpeningPlies != len(pair.opening.Moves) || record.DurationMS < 0 ||
		len(record.Moves) < record.OpeningPlies || len(record.Moves) > config.MaxMoves ||
		len(record.Frames) != len(record.Moves)+1 || record.SHA256 != recordHash(record) {
		return errors.New("game identity, hash, length or schedule mismatch")
	}
	if !reflect.DeepEqual(record.Moves[:record.OpeningPlies], pair.opening.Moves) && record.OpeningPlies != 0 {
		return errors.New("game does not begin with its declared opening")
	}
	g, _ := game.New(config.BoardSize)
	expected := []Frame{frame(g)}
	for ply, move := range record.Moves {
		if err := g.Play(move); err != nil {
			return fmt.Errorf("illegal replay move at ply %d: %w", ply+1, err)
		}
		expected = append(expected, frame(g))
	}
	var result Result
	switch record.Result.Reason {
	case "passes":
		if !g.Over || g.Passes != 2 {
			return errors.New("claimed passing finish without two consecutive passes")
		}
		result = decided(record, "passes", g.Score.Winner, g.Score)
	case "move-cap":
		if g.Over || len(g.Moves) != config.MaxMoves {
			return errors.New("claimed move cap at the wrong ply")
		}
		score := capScore(g)
		g.Over, g.Score = true, score
		result = decided(record, "move-cap", score.Winner, score)
	case "resign":
		if g.Over || len(g.Moves) >= config.MaxMoves {
			return errors.New("resignation after game end")
		}
		g.Resign()
		g.Score = nil
		result = decided(record, "resign", 3-g.Resigned, nil)
	case "forfeit":
		r := record.Result
		if g.Over || len(g.Moves) >= config.MaxMoves || (r.FaultSide != game.Black && r.FaultSide != game.White) ||
			(r.Phase != "startup" && r.Phase != "turn") || (r.Phase == "startup" && len(g.Moves) != record.OpeningPlies) ||
			(r.Phase == "turn" && r.FaultSide != g.Next) {
			return errors.New("forfeit side or phase is inconsistent")
		}
		switch r.Fault {
		case "timeout", "protocol", "process":
			if r.Attempt != nil {
				return errors.New("non-move fault includes an attempted move")
			}
		case "illegal-move":
			if r.Phase != "turn" || r.Attempt == nil || isolated(g).Play(*r.Attempt) == nil {
				return errors.New("claimed illegal move is legal or missing")
			}
		default:
			return errors.New("unknown forfeit fault")
		}
		result = forfeited(g, record, r.FaultSide, r.Fault, r.Phase, r.Attempt)
	default:
		return errors.New("unknown terminal result")
	}
	expected[len(expected)-1] = frame(g)
	if !reflect.DeepEqual(record.Result, result) || !reflect.DeepEqual(record.Frames, expected) {
		return errors.New("result or replay snapshots differ from the rules")
	}
	return nil
}
