// 弈 is a small, self-contained Go game. Run it with: go run .
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
	"time"

	"yi-go/internal/match"
)

//go:embed web
var assets embed.FS

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
		case "/", "/index.html", "/style.css", "/app.js", "/engine.js":
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
	var input match.Request
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
