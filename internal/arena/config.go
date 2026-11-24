package arena

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/Lazarus-coder/yi-go/internal/game"
)

const SchemaVersion = 1
const EngineVersion = "yi-go-arena-v1"

type Opening struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Moves []game.Move `json:"moves"`
}

type PlayerConfig struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Kind    string       `json:"kind"`
	Style   string       `json:"style,omitempty"`
	Version string       `json:"version,omitempty"`
	Policy  string       `json:"policy,omitempty"`
	Driver  DriverConfig `json:"driver,omitempty"`
}

type Config struct {
	SchemaVersion int            `json:"schemaVersion"`
	Title         string         `json:"title"`
	BoardSize     int            `json:"boardSize"`
	Komi          float64        `json:"komi"`
	TurnTimeoutMS int            `json:"turnTimeoutMS"`
	MaxMoves      int            `json:"maxMoves"`
	Rounds        int            `json:"rounds"`
	Openings      []Opening      `json:"openings"`
	Players       []PlayerConfig `json:"players"`
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func ReadConfig(reader io.Reader) (Config, error) {
	return readConfig(reader, true)
}

// ReadReviewedConfig validates a manifest without requiring its local drivers.
func ReadReviewedConfig(reader io.Reader) (Config, error) {
	return readConfig(reader, false)
}

func readConfig(reader io.Reader, drivers bool) (Config, error) {
	var config Config
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return config, errors.New("config must contain one JSON object")
	}
	return config, config.validate(drivers)
}

// Validate checks every participant and opening before starting any match.
func (c Config) Validate() error {
	return c.validate(true)
}

func (c Config) validate(drivers bool) error {
	if c.SchemaVersion != SchemaVersion || strings.TrimSpace(c.Title) == "" || len(c.Title) > 200 {
		return errors.New("config needs schemaVersion 1 and a title of at most 200 bytes")
	}
	if _, err := game.New(c.BoardSize); err != nil {
		return err
	}
	if c.Komi != game.Komi {
		return errors.New("this referee supports exactly 6.5 komi")
	}
	if c.TurnTimeoutMS < 10 || c.TurnTimeoutMS > 120000 || c.MaxMoves < 2 || c.MaxMoves > 2000 || c.Rounds < 1 || c.Rounds > 100 {
		return errors.New("turnTimeoutMS must be 10..120000, maxMoves 2..2000, rounds 1..100")
	}
	if len(c.Players) < 2 || len(c.Players) > 32 || len(c.Openings) < 1 || len(c.Openings) > 16 ||
		len(c.Players)*(len(c.Players)-1)*len(c.Openings)*c.Rounds > 4096 {
		return errors.New("use 2..32 players, 1..16 openings, and at most 4096 paired games")
	}
	ids := map[string]bool{}
	for _, player := range c.Players {
		if !safeID.MatchString(player.ID) || ids[player.ID] || strings.TrimSpace(player.Name) == "" || len(player.Name) > 200 || len(player.Version) > 200 || len(player.Policy) > 2000 {
			return fmt.Errorf("invalid or duplicate player identity %q", player.ID)
		}
		ids[player.ID] = true
		switch player.Kind {
		case "builtin":
			if player.Driver.Mode != "" || len(player.Driver.Command) != 0 || player.Driver.Image != "" || player.Driver.Runtime != "" ||
				player.Driver.StartupTimeoutMS != 0 || player.Driver.MemoryMB != 0 || player.Driver.CPUs != 0 {
				return fmt.Errorf("builtin %s cannot declare external execution limits", player.ID)
			}
			if player.Style == "" {
				return fmt.Errorf("builtin %s needs a style", player.ID)
			}
			if _, err := game.NormalizeStyle(player.Style); err != nil {
				return err
			}
		case "external":
			if strings.TrimSpace(player.Version) == "" || player.Style != "" {
				return fmt.Errorf("external %s needs a version and no builtin style", player.ID)
			}
			if drivers {
				if err := ValidateDriver(player.Driver); err != nil {
					return fmt.Errorf("player %s: %w", player.ID, err)
				}
			}
		default:
			return fmt.Errorf("unknown player kind %q", player.Kind)
		}
	}
	ids = map[string]bool{}
	for _, opening := range c.Openings {
		if !safeID.MatchString(opening.ID) || ids[opening.ID] || strings.TrimSpace(opening.Name) == "" || len(opening.Name) > 200 || len(opening.Moves) >= c.MaxMoves {
			return fmt.Errorf("invalid or duplicate opening %q", opening.ID)
		}
		ids[opening.ID] = true
		g, _ := game.New(c.BoardSize)
		for ply, move := range opening.Moves {
			if err := g.Play(move); err != nil || g.Over {
				return fmt.Errorf("opening %s is illegal or terminal at ply %d", opening.ID, ply+1)
			}
		}
	}
	return nil
}

func digest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
