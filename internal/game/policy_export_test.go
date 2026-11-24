package game

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"testing"
)

// Optional offline training export; ordinary builds only need the learned weights.
func TestExportPolicyTraining(t *testing.T) {
	input := os.Getenv("YI_POLICY_GAMES")
	if input == "" {
		t.Skip("set YI_POLICY_GAMES and YI_POLICY_EXAMPLES to export teacher positions")
	}
	source, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	file, err := os.Create(os.Getenv("YI_POLICY_EXAMPLES"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	buffer := bufio.NewWriterSize(file, 1<<20)
	write, read := json.NewEncoder(buffer), json.NewDecoder(source)
	for {
		var record struct {
			Game, Size, Opening int
			Moves               []string
		}
		if err := read.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		g, err := New(record.Size)
		if err != nil {
			t.Fatal(err)
		}
		for ply, coordinate := range record.Moves {
			move, err := ParseMove(coordinate, g.Size)
			if err != nil {
				t.Fatal(err)
			}
			if ply >= record.Opening && move.X >= 0 {
				var inputs [maxPoints]policyFeatures
				ranked := g.rankCandidates("alphago", func(m Move, f policyFeatures, heuristic float64) float64 {
					inputs[m.Y*g.Size+m.X] = f
					return heuristic
				})
				example := struct {
					Game     int              `json:"game"`
					Size     int              `json:"size"`
					Target   int              `json:"target"`
					Features []policyFeatures `json:"features"`
				}{Game: record.Game, Size: g.Size, Target: -1, Features: make([]policyFeatures, 0, len(ranked))}
				for i, candidate := range ranked {
					if candidate.move == move {
						example.Target = i
					}
					example.Features = append(example.Features, inputs[candidate.move.Y*g.Size+candidate.move.X])
				}
				if example.Target >= 0 {
					if err := write.Encode(example); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := g.Play(move); err != nil {
				t.Fatalf("game %d ply %d, %s: %v", record.Game, ply, coordinate, err)
			}
		}
	}
	if err := buffer.Flush(); err != nil {
		t.Fatal(err)
	}
}
