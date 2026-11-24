package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"yi-go/internal/game"
)

func callTable(t *testing.T, handler http.Handler, method, path string, input any, key string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, bytes.NewReader(body))
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func createTestTable(t *testing.T, handler http.Handler, opponent string) createdTable {
	t.Helper()
	w := callTable(t, handler, http.MethodPost, "/api/tables", map[string]any{"size": 9, "opponent": opponent}, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var created createdTable
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	return created
}

func readTestTable(t *testing.T, handler http.Handler, id string) tableState {
	t.Helper()
	w := callTable(t, handler, http.MethodGet, "/api/tables/"+id, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("read: %d %s", w.Code, w.Body)
	}
	var state tableState
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestAgentTableSeatsAndSpectators(t *testing.T) {
	handler := newHandler()
	created := createTestTable(t, handler, "agent")
	if len(created.ID) != 32 || len(created.BlackKey) != 64 || len(created.WhiteKey) != 64 || created.BlackKey == created.WhiteKey {
		t.Fatal("table ID and independent seat keys must be cryptographic tokens")
	}
	if created.WatchURL != "/watch.html?table="+created.ID || created.State.Revision != 0 || created.State.Turn != "black" {
		t.Fatalf("unexpected new table: %+v", created.State)
	}
	if len(created.State.Legal) != 83 || !slices.Contains(created.State.Legal, "D4") || !slices.Contains(created.State.Legal, "pass") || !slices.Contains(created.State.Legal, "resign") {
		t.Fatal("empty board should list every move, pass and resign")
	}
	path := "/api/tables/" + created.ID + "/moves"
	for _, tc := range []struct {
		key  string
		want int
	}{{"", http.StatusUnauthorized}, {"wrong", http.StatusUnauthorized}, {created.WhiteKey, http.StatusForbidden}} {
		w := callTable(t, handler, http.MethodPost, path, map[string]any{"revision": 0, "move": "D4"}, tc.key)
		if w.Code != tc.want {
			t.Fatalf("seat authorization: got %d, want %d", w.Code, tc.want)
		}
	}
	w := callTable(t, handler, http.MethodPost, path, map[string]any{"revision": 0, "move": "D4"}, created.BlackKey)
	if w.Code != http.StatusOK {
		t.Fatalf("black move: %d %s", w.Code, w.Body)
	}
	state := readTestTable(t, handler, created.ID)
	if state.Revision != 1 || state.Turn != "white" || state.Game.Board[5*9+3] != game.Black || len(state.Game.Moves) != 1 || slices.Contains(state.Legal, "D4") {
		t.Fatalf("wrong state after D4: %+v", state)
	}
	if !strings.Contains(state.ASCII, "X = black") || !strings.Contains(state.ASCII, " 4 . . . X ") {
		t.Fatalf("ASCII board lacks D4: %s", state.ASCII)
	}
	w = callTable(t, handler, http.MethodGet, "/api/tables/"+created.ID, nil, "")
	if strings.Contains(w.Body.String(), created.BlackKey) || strings.Contains(w.Body.String(), created.WhiteKey) || strings.Contains(w.Body.String(), "Key") {
		t.Fatal("spectator response leaked a seat key")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("table responses should not be cached")
	}
	w = callTable(t, handler, http.MethodPost, path, map[string]any{"revision": 0, "move": "E4"}, created.WhiteKey)
	if w.Code != http.StatusConflict {
		t.Fatal("stale revision was accepted")
	}
	w = callTable(t, handler, http.MethodPost, path, map[string]any{"revision": 1, "move": "D4"}, created.WhiteKey)
	if w.Code != http.StatusBadRequest || readTestTable(t, handler, created.ID).Revision != 1 {
		t.Fatal("illegal move should leave the table unchanged")
	}
	w = callTable(t, handler, http.MethodPost, path, map[string]any{"revision": 1, "move": "resign"}, created.WhiteKey)
	state = readTestTable(t, handler, created.ID)
	if w.Code != http.StatusOK || state.Turn != "over" || state.Revision != 2 || state.Game.Resigned != game.White || state.Game.Score.Winner != game.Black || len(state.Legal) != 0 {
		t.Fatalf("white resignation failed: %+v", state)
	}
}

func TestAgentTableCPUTurnIsAtomic(t *testing.T) {
	handler := newHandler()
	created := createTestTable(t, handler, "japan")
	if created.WhiteKey != "" || created.State.Game.Style != "japan" || created.State.Opponent != "japan" {
		t.Fatal("CPU table must keep its profile and have only a black key")
	}
	path := "/api/tables/" + created.ID + "/moves"
	w := callTable(t, handler, http.MethodPost, path, map[string]any{"revision": 0, "move": "d4"}, created.BlackKey)
	state := readTestTable(t, handler, created.ID)
	if w.Code != http.StatusOK || state.Revision != 1 || state.Turn != "black" || len(state.Game.Moves) != 2 || state.Game.Board[5*9+3] != game.Black {
		t.Fatalf("CPU failed to complete the turn: %d %+v", w.Code, state)
	}
	before, _ := json.Marshal(state)
	w = callTable(t, handler, http.MethodPost, path, map[string]any{"revision": 0, "move": "E4"}, created.BlackKey)
	after, _ := json.Marshal(readTestTable(t, handler, created.ID))
	if w.Code != http.StatusConflict || !bytes.Equal(before, after) {
		t.Fatal("retry with an old revision mutated the table")
	}
}

func TestAgentTableConcurrentMovesAndIsolation(t *testing.T) {
	handler := newHandler()
	created := createTestTable(t, handler, "agent")
	other := createTestTable(t, handler, "agent")
	path := "/api/tables/" + created.ID + "/moves"
	w := callTable(t, handler, http.MethodPost, path, map[string]any{"revision": 0, "move": "D4"}, other.BlackKey)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("a different table's key was accepted")
	}
	var group sync.WaitGroup
	statuses := make(chan int, 8)
	group.Add(1)
	go func() {
		defer group.Done()
		for range 8 {
			readTestTable(t, handler, created.ID)
		}
	}()
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			w := callTable(t, handler, http.MethodPost, path, map[string]any{"revision": 0, "move": "D4"}, created.BlackKey)
			statuses <- w.Code
		}()
	}
	group.Wait()
	close(statuses)
	accepted := 0
	for status := range statuses {
		if status == http.StatusOK {
			accepted++
		} else if status != http.StatusConflict {
			t.Fatalf("unexpected concurrent response: %d", status)
		}
	}
	state := readTestTable(t, handler, created.ID)
	if accepted != 1 || state.Revision != 1 || len(state.Game.Moves) != 1 || readTestTable(t, handler, other.ID).Revision != 0 {
		t.Fatal("concurrent duplicate moves were not isolated")
	}
}

func TestAgentTableLegalMovesRespectRulesWithoutMutation(t *testing.T) {
	g, _ := game.New(9)
	for _, move := range []game.Move{{X: 0, Y: 1}, {X: 1, Y: 0}, {X: 2, Y: 1}, {X: 1, Y: 2}} {
		g.Board[move.Y*9+move.X] = game.White
	}
	table := &agentTable{game: g, opponent: "agent"}
	before, _ := json.Marshal(g)
	if slices.Contains(table.state().Legal, "B8") || slices.Contains(table.state().Legal, "A8") {
		t.Fatal("suicide and occupied points must not be offered")
	}
	after, _ := json.Marshal(g)
	if !bytes.Equal(before, after) {
		t.Fatal("listing legal moves mutated the game")
	}
	g, _ = game.New(9)
	for _, move := range []game.Move{{X: 0, Y: 1}, {X: 1, Y: 0}, {X: 2, Y: 1}} {
		g.Board[move.Y*9+move.X] = game.Black
	}
	for _, move := range []game.Move{{X: 1, Y: 1}, {X: 0, Y: 2}, {X: 2, Y: 2}, {X: 1, Y: 3}} {
		g.Board[move.Y*9+move.X] = game.White
	}
	if err := g.Play(game.Move{X: 1, Y: 2}); err != nil {
		t.Fatal(err)
	}
	table.game = g
	if slices.Contains(table.state().Legal, "B8") || g.IsLegal(game.Move{X: 1, Y: 1}) {
		t.Fatal("immediate ko recapture must not be offered")
	}
}

func TestAgentTablePassesAndMoveLimit(t *testing.T) {
	mux := http.NewServeMux()
	tables := registerAgentTables(mux)
	created := createTestTable(t, mux, "agent")
	path := "/api/tables/" + created.ID + "/moves"
	for i, key := range []string{created.BlackKey, created.WhiteKey} {
		w := callTable(t, mux, http.MethodPost, path, map[string]any{"revision": i, "move": "pass"}, key)
		if w.Code != http.StatusOK {
			t.Fatalf("pass: %d %s", w.Code, w.Body)
		}
	}
	state := readTestTable(t, mux, created.ID)
	if state.Turn != "over" || state.Game.Score == nil || len(state.Legal) != 0 {
		t.Fatal("two passes should end an agent game")
	}
	created = createTestTable(t, mux, "agent")
	table := tables.lock(created.ID)
	table.game.Moves = make([]game.Move, maxTableMoves)
	table.mu.Unlock()
	path = "/api/tables/" + created.ID + "/moves"
	w := callTable(t, mux, http.MethodPost, path, map[string]any{"revision": 0, "move": "D4"}, created.BlackKey)
	if w.Code != http.StatusConflict {
		t.Fatal("move limit was not enforced")
	}
	state = readTestTable(t, mux, created.ID)
	if !slices.Equal(state.Legal, []string{"resign"}) {
		t.Fatalf("move limit should still allow resignation: %v", state.Legal)
	}
	w = callTable(t, mux, http.MethodPost, path, map[string]any{"revision": 0, "move": "resign"}, created.BlackKey)
	if w.Code != http.StatusOK {
		t.Fatal("resignation should work at the move limit")
	}
}

func TestAgentTableCapacityAndReclamation(t *testing.T) {
	mux := http.NewServeMux()
	tables := registerAgentTables(mux)
	first := createTestTable(t, mux, "agent")
	second := createTestTable(t, mux, "agent")
	for len(tables.tables) < maxAgentTables {
		createTestTable(t, mux, "agent")
	}
	w := callTable(t, mux, http.MethodPost, "/api/tables", map[string]any{"size": 9, "opponent": "agent"}, "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("table registry exceeded its bound")
	}
	table := tables.lock(first.ID)
	table.updated = time.Now().Add(-25 * time.Hour)
	table.mu.Unlock()
	table = tables.lock(second.ID)
	table.game.Resign()
	table.updated = time.Now().Add(-2 * time.Hour)
	table.mu.Unlock()
	createTestTable(t, mux, "agent")
	if len(tables.tables) != maxAgentTables-1 {
		t.Fatal("stale and old finished tables were not reclaimed")
	}
	for _, id := range []string{first.ID, second.ID} {
		w := callTable(t, mux, http.MethodGet, "/api/tables/"+id, nil, "")
		if w.Code != http.StatusNotFound {
			t.Fatal("reclaimed table remains readable")
		}
	}
}

func TestAgentTableHTTPBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, body, host, origin, media string
		status                          int
	}{
		{"valid", `{"size":9,"opponent":"agent"}`, "127.0.0.1:8080", "http://127.0.0.1:8080", "application/json; charset=utf-8", 201},
		{"localhost", `{"size":9,"opponent":"china"}`, "localhost:8080", "", "application/json", 201},
		{"ipv6", `{"size":9,"opponent":"agent"}`, "[::1]:8080", "", "application/json", 201},
		{"cross origin", `{}`, "127.0.0.1:8080", "https://example.com", "application/json", 403},
		{"wrong scheme", `{}`, "127.0.0.1:8080", "https://127.0.0.1:8080", "application/json", 403},
		{"rebinding host", `{}`, "attacker.example", "http://attacker.example", "application/json", 403},
		{"wrong media", `{}`, "127.0.0.1:8080", "", "text/plain", 415},
		{"missing media", `{}`, "127.0.0.1:8080", "", "", 415},
		{"unknown key", `{"size":9,"opponent":"agent","key":"secret"}`, "127.0.0.1:8080", "", "application/json", 400},
		{"trailing JSON", `{"size":9,"opponent":"agent"} {}`, "127.0.0.1:8080", "", "application/json", 400},
		{"too large", strings.Repeat(" ", 4<<10) + `{}`, "127.0.0.1:8080", "", "application/json", 400},
		{"invalid size", `{"size":8,"opponent":"agent"}`, "127.0.0.1:8080", "", "application/json", 400},
		{"invalid opponent", `{"size":9,"opponent":"unknown"}`, "127.0.0.1:8080", "", "application/json", 400},
		{"missing opponent", `{"size":9}`, "127.0.0.1:8080", "", "application/json", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/api/tables", strings.NewReader(tc.body))
			r.RemoteAddr = "127.0.0.1:50000"
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", tc.media)
			w := httptest.NewRecorder()
			newHandler().ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", w.Code, tc.status, w.Body)
			}
		})
	}
	handler := newHandler()
	created := createTestTable(t, handler, "agent")
	for _, input := range []any{
		map[string]any{"move": "D4"},
		map[string]any{"revision": -1, "move": "D4"},
		map[string]any{"revision": 0, "move": "I4"},
		map[string]any{"revision": 0, "move": "T19"},
		map[string]any{"revision": 0, "move": "A01"},
		map[string]any{"revision": 0, "move": "D0"},
	} {
		w := callTable(t, handler, http.MethodPost, "/api/tables/"+created.ID+"/moves", input, created.BlackKey)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid move request accepted: %d %s", w.Code, w.Body)
		}
	}
	if readTestTable(t, handler, created.ID).Revision != 0 {
		t.Fatal("boundary rejection changed game state")
	}
}

func TestTableRejectsRemoteClientWithLocalHostHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://localhost/api/tables/example", nil)
	r.RemoteAddr = "192.0.2.1:50000"
	w := httptest.NewRecorder()
	newHandler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("expected a loopback peer")
	}
}
