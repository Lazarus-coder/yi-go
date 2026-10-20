// 弈 is a small, self-contained Go game. Run it with: go run .
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"time"

	"yi-go/internal/game"
)

//go:embed web
var assets embed.FS

type request struct {
	Size     int         `json:"size"`
	Moves    []game.Move `json:"moves"`
	Action   string      `json:"action"`
	X        *int        `json:"x,omitempty"`
	Y        *int        `json:"y,omitempty"`
	Resigned int         `json:"resigned,omitempty"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()
	server := &http.Server{
		Addr:              *addr,
		Handler:           newHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	fmt.Printf("弈 · http://%s\n按 Ctrl+C 退出\n", *addr)
	log.Fatal(server.ListenAndServe())
}

func newHandler() http.Handler {
	files, err := fs.Sub(assets, "web")
	if err != nil {
		panic(err)
	}
	static := http.FileServer(http.FS(files))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/game", handleGame)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/index.html", "/style.css", "/app.js":
			static.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache")
		mux.ServeHTTP(w, r)
	})
}

func handleGame(w http.ResponseWriter, r *http.Request) {
	// The local app has no cross-origin clients.
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
			writeError(w, http.StatusForbidden, "请从应用页面发起请求。")
			return
		}
	}
	var input request
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确。")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "请求只能包含一局棋。")
		return
	}
	g, err := restore(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch input.Action {
	case "sync":
		// Replaying history also implements refresh, undo, and new games.
	case "play", "pass", "resign":
		if g.Over || g.Next != game.Black {
			writeError(w, http.StatusConflict, "当前不能落子，请开始新对局或悔棋。")
			return
		}
		if input.Action == "resign" {
			g.Resign()
			break
		}
		if len(g.Moves)+2 > 2000 {
			writeError(w, http.StatusBadRequest, "本局已达 2000 手上限，请悔棋或开始新对局。")
			return
		}
		move := game.Move{X: -1, Y: -1}
		if input.Action == "play" {
			if input.X == nil || input.Y == nil {
				writeError(w, http.StatusBadRequest, "请选择棋盘上的交叉点。")
				return
			}
			move = game.Move{X: *input.X, Y: *input.Y}
			if move.X < 0 || move.Y < 0 {
				writeError(w, http.StatusBadRequest, "落子坐标超出棋盘。")
				return
			}
		}
		if err := g.Play(move); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !g.Over {
			if err := g.Play(g.ChooseMove()); err != nil {
				log.Printf("CPU produced an illegal move: %v", err)
				writeError(w, http.StatusInternalServerError, "CPU 暂时无法落子，请重试。")
				return
			}
		}
	default:
		writeError(w, http.StatusBadRequest, "未知操作。")
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func restore(input request) (*game.Game, error) {
	if len(input.Moves) > 2000 {
		return nil, fmt.Errorf("棋谱超过示范版的 2000 手上限。")
	}
	g, err := game.New(input.Size)
	if err != nil {
		return nil, err
	}
	for i, move := range input.Moves {
		if err := g.Play(move); err != nil {
			return nil, fmt.Errorf("棋谱第 %d 手无效：%w", i+1, err)
		}
	}
	if !g.Over && g.Next != game.Black {
		return nil, fmt.Errorf("棋谱缺少 CPU 应手，请使用完整回合的记录。")
	}
	if input.Resigned != 0 {
		if input.Resigned != game.Black || g.Next != game.Black || g.Over {
			return nil, fmt.Errorf("认输记录无效。")
		}
		g.Resign()
	}
	return g, nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}
