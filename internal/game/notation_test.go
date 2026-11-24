package game

import (
	"slices"
	"strings"
	"testing"
)

func TestAgentNotationAndLegalObservation(t *testing.T) {
	for _, size := range []int{9, 13, 19} {
		g, _ := New(size)
		if len(LegalMoves(g)) != size*size+2 {
			t.Fatalf("missing legal moves on %d board", size)
		}
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				move := Move{X: x, Y: y}
				parsed, err := ParseMove(strings.ToLower(FormatMove(move, size)), size)
				if err != nil || parsed != move {
					t.Fatalf("coordinate round trip failed: %+v %v", move, err)
				}
			}
		}
		g.Play(Move{X: 0, Y: 0})
		if slices.Contains(LegalMoves(g), FormatMove(Move{X: 0, Y: 0}, size)) || !strings.Contains(ASCII(g), "X = black") {
			t.Fatal("observation lost occupancy or legend")
		}
		g.Resign()
		if len(LegalMoves(g)) != 0 {
			t.Fatal("finished game offers moves")
		}
	}
	for _, invalid := range []string{"I1", "A0", "A01", "Z1", "A20", "A+1", "pass extra"} {
		if _, err := ParseMove(invalid, 19); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	if move, err := ParseMove("pass", 9); err != nil || FormatMove(move, 9) != "pass" {
		t.Fatal("pass did not round trip")
	}
}
