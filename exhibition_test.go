package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lazarus-coder/yi-go/internal/match"
)

func TestExhibitionHTTP(t *testing.T) {
	h := newHandler(nil)
	for _, tc := range []struct {
		name, body, origin string
		status             int
	}{
		{"finished", `{"maxMoves":2,"history":["C7","G3"]}`, "", 200},
		{"extra ply", `{"maxMoves":4,"history":["pass","pass","E5"]}`, "", 400},
		{"secret field", `{"maxMoves":80,"key":"dummy"}`, "", 400},
		{"cross origin", `{"maxMoves":80}`, "https://elsewhere.example", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/exhibition", strings.NewReader(tc.body))
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d: %s", w.Code, w.Body)
			}
			if tc.status == 200 {
				var response match.Exhibition
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Result == nil || response.Result.Reason != "move-cap" || response.Result.Winner != "white" || response.Result.Score.Margin != 6.5 || !response.Game.Over || len(response.Game.Moves) != 2 {
					t.Fatalf("unexpected capped match: %+v", response)
				}
			}
		})
	}
}
