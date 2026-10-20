package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yi-go/internal/game"
)

func post(t *testing.T, input any) (*httptest.ResponseRecorder, *game.Game) {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/game", bytes.NewReader(body))
	w := httptest.NewRecorder()
	newHandler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		return w, nil
	}
	var g game.Game
	if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	return w, &g
}

func TestPlayRestoreAndUndo(t *testing.T) {
	x, y := 2, 2
	w, played := post(t, request{Size: 9, Action: "play", X: &x, Y: &y})
	if played == nil {
		t.Fatalf("play: %d %s", w.Code, w.Body)
	}
	if len(played.Moves) != 2 || played.Board[y*9+x] != game.Black || played.Next != game.Black {
		t.Fatalf("expected a human move and CPU reply: %+v", played)
	}
	_, restored := post(t, request{Size: 9, Action: "sync", Moves: played.Moves})
	if restored == nil {
		t.Fatal("restore failed")
	}
	a, _ := json.Marshal(played)
	b, _ := json.Marshal(restored)
	if !bytes.Equal(a, b) {
		t.Fatalf("restore changed game:\n%s\n%s", a, b)
	}
	w, _ = post(t, request{Size: 9, Action: "play", Moves: played.Moves, X: &x, Y: &y})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("occupied point accepted: %d", w.Code)
	}
	_, undone := post(t, request{Size: 9, Action: "sync", Moves: played.Moves[:0]})
	if undone == nil || len(undone.Moves) != 0 || undone.Board[y*9+x] != game.Empty {
		t.Fatal("undo did not restore the empty board")
	}
}

func TestResignAndRestore(t *testing.T) {
	w, resigned := post(t, request{Size: 13, Action: "resign"})
	if resigned == nil || !resigned.Over || resigned.Resigned != game.Black || resigned.Score.Winner != game.White {
		t.Fatalf("resign failed: %d %s", w.Code, w.Body)
	}
	_, restored := post(t, request{Size: 13, Action: "sync", Resigned: game.Black})
	if restored == nil || !restored.Over || restored.Resigned != game.Black {
		t.Fatal("resigned game was not restored")
	}
	w, _ = post(t, request{Size: 13, Action: "pass", Resigned: game.Black})
	if w.Code != http.StatusConflict {
		t.Fatal("accepted a move after resignation")
	}
}

func TestPassAndFinishedGame(t *testing.T) {
	// A complete recorded game can be restored without invoking the CPU.
	moves := []game.Move{{X: -1, Y: -1}, {X: -1, Y: -1}}
	w, g := post(t, request{Size: 9, Action: "sync", Moves: moves})
	if g == nil || !g.Over || g.Score == nil || g.Score.White != 6.5 {
		t.Fatalf("two passes: %d %s", w.Code, w.Body)
	}
	w, _ = post(t, request{Size: 9, Action: "pass", Moves: moves})
	if w.Code != http.StatusConflict {
		t.Fatal("accepted move after two passes")
	}
	w, g = post(t, request{Size: 9, Action: "pass"})
	if g == nil || len(g.Moves) != 2 || g.Moves[0] != (game.Move{X: -1, Y: -1}) {
		t.Fatalf("human pass should receive CPU reply: %d %s", w.Code, w.Body)
	}
}

func TestInvalidRequests(t *testing.T) {
	zero, negative, outside := 0, -1, 19
	cases := []request{
		{Size: 8, Action: "sync"},
		{Size: 9, Action: "unknown"},
		{Size: 9, Action: "play"},
		{Size: 9, Action: "play", X: &zero},
		{Size: 19, Action: "play", X: &outside, Y: &zero},
		{Size: 9, Action: "play", X: &negative, Y: &negative},
		{Size: 9, Action: "sync", Moves: []game.Move{{X: 0, Y: 0}}},
		{Size: 9, Action: "sync", Moves: []game.Move{{X: 0, Y: 0}, {X: 0, Y: 0}}},
		{Size: 9, Action: "sync", Moves: make([]game.Move, 2001)},
		{Size: 9, Action: "sync", Resigned: game.White},
	}
	for _, input := range cases {
		w, _ := post(t, input)
		if w.Code < 400 {
			t.Errorf("accepted invalid request: %+v", input)
		}
	}
}

func TestHTTPBoundary(t *testing.T) {
	cases := []struct {
		name, body, origin string
		status             int
	}{
		{"valid", `{"size":9,"action":"sync"}`, "", 200},
		{"same origin", `{"size":9,"action":"sync"}`, "http://example.com", 200},
		{"cross origin", `{"size":9,"action":"sync"}`, "https://elsewhere.example", 403},
		{"unknown key", `{"size":9,"action":"sync","extra":true}`, "", 400},
		{"trailing JSON", `{"size":9,"action":"sync"} {}`, "", 400},
		{"malformed", `{"size":`, "", 400},
		{"large", strings.Repeat(" ", 64<<10) + `{}`, "", 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/game", strings.NewReader(tc.body))
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			newHandler().ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", w.Code, tc.status, w.Body)
			}
			if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
				t.Fatal("API response is not JSON")
			}
		})
	}
}

func TestEmbeddedPage(t *testing.T) {
	for _, path := range []string{"/", "/style.css", "/app.js"} {
		w := httptest.NewRecorder()
		newHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Errorf("embedded asset %s unavailable: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	newHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/main.go", nil))
	if w.Code != 404 {
		t.Fatal("non-web source path should not be served")
	}
}
