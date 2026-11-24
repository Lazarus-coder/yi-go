package arena

import (
	"runtime"
	"sort"

	"github.com/Lazarus-coder/yi-go/internal/game"
)

type PublicConfig struct {
	Title         string    `json:"title"`
	BoardSize     int       `json:"boardSize"`
	Komi          float64   `json:"komi"`
	TurnTimeoutMS int       `json:"turnTimeoutMS"`
	MaxMoves      int       `json:"maxMoves"`
	Rounds        int       `json:"rounds"`
	PairedColors  bool      `json:"pairedColors"`
	MoveCapPolicy string    `json:"moveCapPolicy"`
	Openings      []Opening `json:"openings"`
}

type Identity struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Style     string    `json:"style,omitempty"`
	Version   string    `json:"version,omitempty"`
	Policy    string    `json:"policy,omitempty"`
	Execution Execution `json:"execution"`
}

// Execution publishes effective resource limits, never launch paths or secrets.
type Execution struct {
	Mode             string  `json:"mode"`
	StartupTimeoutMS int     `json:"startupTimeoutMS,omitempty"`
	Runtime          string  `json:"runtime,omitempty"`
	MemoryMB         int     `json:"memoryMB,omitempty"`
	CPUs             float64 `json:"cpus,omitempty"`
}

type Provenance struct {
	EngineVersion  string `json:"engineVersion"`
	ManifestSHA256 string `json:"manifestSHA256"`
	GoVersion      string `json:"goVersion"`
}

type Frame struct {
	Board    string        `json:"board"`
	Next     int           `json:"next"`
	Captures game.Captures `json:"captures"`
	Passes   int           `json:"passes"`
	LastMove *game.Move    `json:"lastMove"`
	Over     bool          `json:"over"`
	Resigned int           `json:"resigned,omitempty"`
	Score    *game.Score   `json:"score,omitempty"`
}

type Result struct {
	Winner      string      `json:"winner"`
	WinnerColor int         `json:"winnerColor"`
	Reason      string      `json:"reason"`
	Fault       string      `json:"fault,omitempty"`
	FaultSide   int         `json:"faultSide,omitempty"`
	Phase       string      `json:"phase,omitempty"`
	Attempt     *game.Move  `json:"attempt,omitempty"`
	Score       *game.Score `json:"score,omitempty"`
}

type GameRecord struct {
	ID           string      `json:"id"`
	OpeningID    string      `json:"openingID"`
	Round        int         `json:"round"`
	Black        string      `json:"black"`
	White        string      `json:"white"`
	Size         int         `json:"size"`
	Komi         float64     `json:"komi"`
	OpeningPlies int         `json:"openingPlies"`
	Moves        []game.Move `json:"moves"`
	Frames       []Frame     `json:"frames"`
	Result       Result      `json:"result"`
	DurationMS   int64       `json:"durationMS"`
	SHA256       string      `json:"sha256"`
}

type Standing struct {
	PlayerID string  `json:"playerID"`
	Played   int     `json:"played"`
	Wins     int     `json:"wins"`
	Losses   int     `json:"losses"`
	Draws    int     `json:"draws"`
	Forfeits int     `json:"forfeits"`
	MoveCaps int     `json:"moveCaps"`
	Points   float64 `json:"points"`
	WinRate  float64 `json:"winRate"`
}

type Report struct {
	SchemaVersion int          `json:"schemaVersion"`
	GeneratedAt   string       `json:"generatedAt"`
	Config        PublicConfig `json:"config"`
	Provenance    Provenance   `json:"provenance"`
	Players       []Identity   `json:"players"`
	Games         []GameRecord `json:"games"`
	Standings     []Standing   `json:"standings"`
}

func manifest(c Config) (PublicConfig, []Identity) {
	config := PublicConfig{Title: c.Title, BoardSize: c.BoardSize, Komi: c.Komi,
		TurnTimeoutMS: c.TurnTimeoutMS, MaxMoves: c.MaxMoves, Rounds: c.Rounds,
		PairedColors: true, MoveCapPolicy: "area-score", Openings: append([]Opening(nil), c.Openings...)}
	for i := range config.Openings {
		config.Openings[i].Moves = append([]game.Move{}, config.Openings[i].Moves...)
	}
	players := make([]Identity, 0, len(c.Players))
	for _, p := range c.Players {
		version := p.Version
		if version == "" {
			version = EngineVersion
		}
		players = append(players, Identity{ID: p.ID, Name: p.Name, Kind: p.Kind, Style: p.Style, Version: version, Policy: p.Policy, Execution: execution(p)})
	}
	return config, players
}

func execution(player PlayerConfig) Execution {
	if player.Kind == "builtin" {
		return Execution{Mode: "builtin"}
	}
	driver := player.Driver
	if driver.StartupTimeoutMS == 0 {
		driver.StartupTimeoutMS = 5000
	}
	result := Execution{Mode: driver.Mode, StartupTimeoutMS: driver.StartupTimeoutMS}
	if driver.Mode == "oci" {
		result.Runtime, result.MemoryMB, result.CPUs = driver.Runtime, driver.MemoryMB, driver.CPUs
		if result.Runtime == "" {
			result.Runtime = "docker"
		}
		if result.MemoryMB == 0 {
			result.MemoryMB = 256
		}
		if result.CPUs == 0 {
			result.CPUs = 1
		}
	}
	return result
}

func manifestHash(config PublicConfig, players []Identity) string {
	return digest(struct {
		Config  PublicConfig `json:"config"`
		Players []Identity   `json:"players"`
	}{config, players})
}

func newReport(c Config) *Report {
	config, players := manifest(c)
	return &Report{SchemaVersion: SchemaVersion, Config: config, Players: players,
		Provenance: Provenance{EngineVersion, manifestHash(config, players), runtime.Version()},
		Games:      []GameRecord{}, Standings: []Standing{}}
}

func frame(g *game.Game) Frame {
	board := make([]byte, len(g.Board))
	for i, color := range g.Board {
		board[i] = byte('0' + color)
	}
	return Frame{string(board), g.Next, g.Captures, g.Passes, g.LastMove, g.Over, g.Resigned, g.Score}
}

func recordHash(record GameRecord) string {
	record.SHA256 = ""
	return digest(record)
}

func standings(players []Identity, games []GameRecord) []Standing {
	rows := make(map[string]*Standing, len(players))
	for _, player := range players {
		rows[player.ID] = &Standing{PlayerID: player.ID}
	}
	for _, record := range games {
		for color, id := range []string{record.Black, record.White} {
			row := rows[id]
			row.Played++
			if record.Result.WinnerColor == 0 {
				row.Draws++
			} else if record.Result.Winner == id {
				row.Wins++
			} else {
				row.Losses++
			}
			if record.Result.Reason == "forfeit" && record.Result.FaultSide == color+1 {
				row.Forfeits++
			}
			if record.Result.Reason == "move-cap" {
				row.MoveCaps++
			}
		}
	}
	result := make([]Standing, 0, len(rows))
	for _, player := range players {
		row := rows[player.ID]
		row.Points = float64(row.Wins) + 0.5*float64(row.Draws)
		if row.Played > 0 {
			row.WinRate = float64(row.Wins) / float64(row.Played)
		}
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Points != result[j].Points {
			return result[i].Points > result[j].Points
		}
		if result[i].Wins != result[j].Wins {
			return result[i].Wins > result[j].Wins
		}
		return result[i].PlayerID < result[j].PlayerID
	})
	return result
}
