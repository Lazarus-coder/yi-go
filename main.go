// Yi serves the arena results, live agent tables and practice board.
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/Lazarus-coder/yi-go/internal/arena"
	"github.com/Lazarus-coder/yi-go/internal/match"
)

//go:embed web AGENTS.md
var assets embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	reportPath := flag.String("report", "", "show standings and replays from this arena report")
	flag.Parse()
	var report *arena.Report
	if *reportPath != "" {
		file, err := os.Open(*reportPath)
		if err != nil {
			log.Fatal(err)
		}
		report, err = arena.ReadReport(file)
		file.Close()
		if err == nil {
			err = arena.Verify(report, nil)
		}
		if err != nil {
			log.Fatalf("invalid arena report: %v", err)
		}
	}
	server := &http.Server{
		Addr:              *addr,
		Handler:           newHandler(report),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	fmt.Printf("弈 · http://%s\n按 Ctrl+C 退出\n", *addr)
	log.Fatal(server.ListenAndServe())
}

func newHandler(report *arena.Report) http.Handler {
	files, err := fs.Sub(assets, "web")
	if err != nil {
		panic(err)
	}
	static := http.FileServer(http.FS(files))
	arenaFiles, err := fs.Sub(files, "arena")
	if err != nil {
		panic(err)
	}
	showcase := http.FileServer(http.FS(arenaFiles))
	mux := http.NewServeMux()
	registerAgentTables(mux)
	mux.HandleFunc("GET /llms.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		http.ServeFileFS(w, r, assets, "AGENTS.md")
	})
	mux.HandleFunc("POST /api/game", handleGame)
	mux.HandleFunc("POST /api/exhibition", handleExhibition)
	if report != nil {
		mux.HandleFunc("GET /report.json", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, report)
		})
	}
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/index.html", "/arena.css", "/arena.js", "/report.json":
			showcase.ServeHTTP(w, r)
		case "/play.html", "/style.css", "/app.js", "/engine.js", "/board.js", "/board.css", "/kaya.jpg",
			"/watch.html", "/watch.js", "/exhibition.js",
			"/join.html", "/join.css", "/join.js", "/open-referee.js", "/open-players.js", "/replay-fork.js", "/exhibition-save.js":
			static.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections := "connect-src 'self'"
		if r.URL.Path == "/join.html" {
			connections += " https: http://127.0.0.1:* http://localhost:*"
			w.Header().Set("Referrer-Policy", "no-referrer")
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; "+connections+"; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache")
		mux.ServeHTTP(w, r)
	})
}

func decodeGameRequest(w http.ResponseWriter, r *http.Request, input any) bool {
	// The local app has no cross-origin clients.
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
			writeError(w, http.StatusForbidden, "请从应用页面发起请求。")
			return false
		}
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确。")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "请求只能包含一局棋。")
		return false
	}
	return true
}

func handleExhibition(w http.ResponseWriter, r *http.Request) {
	var input match.ExhibitionRequest
	if !decodeGameRequest(w, r, &input) {
		return
	}
	state, err := match.InspectExhibition(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func handleGame(w http.ResponseWriter, r *http.Request) {
	var input match.Request
	if !decodeGameRequest(w, r, &input) {
		return
	}
	g, err := match.Run(input)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, match.ErrTurn) {
			status = http.StatusConflict
		} else if errors.Is(err, match.ErrCPU) {
			status = http.StatusInternalServerError
			log.Printf("CPU produced an illegal move: %v", err)
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, g)
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
