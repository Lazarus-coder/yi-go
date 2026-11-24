package match

import (
	"testing"

	"github.com/Lazarus-coder/yi-go/internal/game"
)

func TestExhibitionForkKeepsFullHistoryAndNewBudget(t *testing.T) {
	// A 19×19 replay can already exceed a new exhibition's 144-move limit.
	request := ExhibitionRequest{Size: 19, StartPly: 146, MaxMoves: 148}
	for i := 0; i < 73; i++ {
		move := game.Move{X: 2 * (i % 10), Y: 2 * (i / 10)}
		request.History = append(request.History, game.FormatMove(move, 19), "pass")
	}
	state, err := InspectExhibition(request)
	if err != nil || state.Result != nil || state.Turn["revision"] != 146 || state.Turn["remainingMoves"] != 2 ||
		state.Game.Next != game.Black || state.Game.Passes != 1 || state.Game.Board[0] != game.Black {
		t.Fatalf("fork did not restore the original position and budget: %+v %v", state, err)
	}
	request.History = append(request.History, "T1", "A1")
	state, err = InspectExhibition(request)
	if err != nil || state.Result == nil || state.Result.Reason != "move-cap" || state.Turn["remainingMoves"] != 0 {
		t.Fatalf("continuation did not end at its own limit: %+v %v", state, err)
	}
}

func TestExhibitionValidatesForkAndCPU(t *testing.T) {
	for _, request := range []ExhibitionRequest{
		{StartPly: -1, MaxMoves: 80}, {StartPly: 1, MaxMoves: 80}, {StartPly: 2001, MaxMoves: 2003},
		{StartPly: 2, MaxMoves: 4, History: []string{"D4", "D4"}},
		{StartPly: 2, MaxMoves: 4, History: []string{"pass", "pass"}},
		{StartPly: 1, MaxMoves: 3, History: []string{"resign"}},
		{StartPly: 1, MaxMoves: 146, History: []string{"D4"}},
		{Size: 8, MaxMoves: 80}, {MaxMoves: 80, ChooseCPU: true, Style: "unknown"},
	} {
		if _, err := InspectExhibition(request); err == nil {
			t.Errorf("accepted invalid request %+v", request)
		}
	}
}

func TestCPUActionUsesSideToMove(t *testing.T) {
	input := Request{Size: 9, Action: "cpu", Style: "alphago"}
	for _, side := range []int{game.Black, game.White} {
		next, err := Run(input)
		if err != nil || len(next.Moves) != len(input.Moves)+1 || next.LastMove == nil ||
			next.Board[next.LastMove.Y*next.Size+next.LastMove.X] != side {
			t.Fatalf("expected a legal neural point by side %d: %+v %v", side, next, err)
		}
		input.Moves = next.Moves
	}
	input.Moves = []game.Move{{X: -1, Y: -1}, {X: -1, Y: -1}}
	if _, err := Run(input); err == nil {
		t.Fatal("CPU was asked to move on a finished board")
	}
}
