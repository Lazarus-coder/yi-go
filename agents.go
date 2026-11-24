package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"yi-go/internal/game"
)

const (
	maxAgentTables = 64
	maxTableMoves  = 2000
	boardColumns   = "ABCDEFGHJKLMNOPQRST"
)

type agentTable struct {
	mu                 sync.Mutex
	game               *game.Game
	revision           int
	opponent           string
	blackKey, whiteKey string
	updated            time.Time
}

type agentTables struct {
	mu     sync.Mutex
	tables map[string]*agentTable
}

type tableState struct {
	Game     *game.Game `json:"game"`
	Revision int        `json:"revision"`
	Turn     string     `json:"turn"`
	Legal    []string   `json:"legal"`
	ASCII    string     `json:"ascii"`
	Opponent string     `json:"opponent"`
}

type createdTable struct {
	ID       string     `json:"id"`
	WatchURL string     `json:"watchURL"`
	BlackKey string     `json:"blackKey"`
	WhiteKey string     `json:"whiteKey,omitempty"`
	State    tableState `json:"state"`
}

func registerAgentTables(mux *http.ServeMux) *agentTables {
	tables := &agentTables{tables: make(map[string]*agentTable)}
	mux.HandleFunc("POST /api/tables", tables.create)
	mux.HandleFunc("GET /api/tables/{id}", tables.read)
	mux.HandleFunc("POST /api/tables/{id}/moves", tables.move)
	return tables
}

func (tables *agentTables) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Size     int    `json:"size"`
		Opponent string `json:"opponent"`
	}
	if !decodeTableRequest(w, r, &input) {
		return
	}
	g, err := game.New(input.Size)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Opponent != "agent" {
		if input.Opponent == "" {
			writeError(w, http.StatusBadRequest, "请选择 agent 或一个 CPU 对手。")
			return
		}
		g.Style, err = game.NormalizeStyle(input.Opponent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	id, err := randomTableToken(16)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "暂时无法创建棋桌。")
		return
	}
	blackKey, err := randomTableToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "暂时无法创建棋桌。")
		return
	}
	whiteKey := ""
	if input.Opponent == "agent" {
		whiteKey, err = randomTableToken(32)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "暂时无法创建棋桌。")
			return
		}
	}
	table := &agentTable{game: g, opponent: input.Opponent, blackKey: blackKey, whiteKey: whiteKey, updated: time.Now()}
	tables.mu.Lock()
	for existingID, existing := range tables.tables {
		existing.mu.Lock()
		age := table.updated.Sub(existing.updated)
		if age > 24*time.Hour || (existing.game.Over && age > time.Hour) {
			delete(tables.tables, existingID)
		}
		existing.mu.Unlock()
	}
	if len(tables.tables) >= maxAgentTables {
		tables.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, "棋桌已满，请稍后重试。")
		return
	}
	table.mu.Lock()
	tables.tables[id] = table
	tables.mu.Unlock()
	defer table.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, createdTable{ID: id, WatchURL: "/watch.html?table=" + id, BlackKey: blackKey, WhiteKey: whiteKey, State: table.state()})
}

// The registry lock stays held until the table is locked, so reclamation cannot
// remove a table between lookup and a move. Callers release the returned lock.
func (tables *agentTables) lock(id string) *agentTable {
	tables.mu.Lock()
	defer tables.mu.Unlock()
	table := tables.tables[id]
	if table != nil {
		table.mu.Lock()
	}
	return table
}

func (tables *agentTables) read(w http.ResponseWriter, r *http.Request) {
	if !tableLocalRequest(w, r) {
		return
	}
	table := tables.lock(r.PathValue("id"))
	if table == nil {
		writeError(w, http.StatusNotFound, "棋桌不存在，或已过期。")
		return
	}
	defer table.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, table.state())
}

func (tables *agentTables) move(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision *int   `json:"revision"`
		Move     string `json:"move"`
	}
	if !decodeTableRequest(w, r, &input) {
		return
	}
	if input.Revision == nil || *input.Revision < 0 {
		writeError(w, http.StatusBadRequest, "请提供当前棋桌的 revision。")
		return
	}
	table := tables.lock(r.PathValue("id"))
	if table == nil {
		writeError(w, http.StatusNotFound, "棋桌不存在，或已过期。")
		return
	}
	defer table.mu.Unlock()
	scheme, key, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	seat := game.Empty
	if strings.EqualFold(scheme, "Bearer") && key != "" {
		if subtle.ConstantTimeCompare([]byte(key), []byte(table.blackKey)) == 1 {
			seat = game.Black
		} else if table.whiteKey != "" && subtle.ConstantTimeCompare([]byte(key), []byte(table.whiteKey)) == 1 {
			seat = game.White
		}
	}
	if seat == game.Empty {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "请提供本棋桌的座位令牌。")
		return
	}
	if table.revision != *input.Revision || table.game.Over {
		writeError(w, http.StatusConflict, "棋局已变化或结束，请重新读取棋桌。")
		return
	}
	if table.game.Next != seat {
		writeError(w, http.StatusForbidden, "尚未轮到这个座位落子。")
		return
	}
	// Play replaces boards; copy the history as well so a CPU failure is atomic.
	next := *table.game
	next.Board = slices.Clone(table.game.Board)
	next.Moves = slices.Clone(table.game.Moves)
	if strings.EqualFold(strings.TrimSpace(input.Move), "resign") {
		next.Resign()
	} else {
		room := 1
		if table.opponent != "agent" {
			room = 2
		}
		if len(next.Moves)+room > maxTableMoves {
			writeError(w, http.StatusConflict, "本局已达 2000 手上限，仍可认输结束。")
			return
		}
		move, err := parseTableMove(input.Move, next.Size)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := next.Play(move); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if table.opponent != "agent" && !next.Over {
			if err := next.Play(next.ChooseMove()); err != nil {
				writeError(w, http.StatusInternalServerError, "CPU 暂时无法落子，棋局未改变。")
				return
			}
		}
	}
	table.game = &next
	table.revision++
	table.updated = time.Now()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, table.state())
}

// state is read and encoded while the table lock is held.
func (table *agentTable) state() tableState {
	g := table.game
	state := tableState{Game: g, Revision: table.revision, Turn: "over", Legal: []string{}, Opponent: table.opponent}
	var ascii strings.Builder
	fmt.Fprint(&ascii, "   ")
	for x := 0; x < g.Size; x++ {
		fmt.Fprintf(&ascii, "%c ", boardColumns[x])
	}
	fmt.Fprintln(&ascii)
	for y := 0; y < g.Size; y++ {
		fmt.Fprintf(&ascii, "%2d ", g.Size-y)
		for x := 0; x < g.Size; x++ {
			fmt.Fprintf(&ascii, "%c ", ".XO"[g.Board[y*g.Size+x]])
		}
		fmt.Fprintf(&ascii, "%d\n", g.Size-y)
	}
	fmt.Fprint(&ascii, "X = black, O = white, . = empty")
	state.ASCII = ascii.String()
	if !g.Over {
		state.Turn = "black"
		if g.Next == game.White {
			state.Turn = "white"
		}
		room := 1
		if table.opponent != "agent" {
			room = 2
		}
		if len(g.Moves)+room <= maxTableMoves {
			for y := 0; y < g.Size; y++ {
				for x := 0; x < g.Size; x++ {
					if g.IsLegal(game.Move{X: x, Y: y}) {
						state.Legal = append(state.Legal, fmt.Sprintf("%c%d", boardColumns[x], g.Size-y))
					}
				}
			}
			state.Legal = append(state.Legal, "pass")
		}
		state.Legal = append(state.Legal, "resign")
	}
	return state
}

func parseTableMove(value string, size int) (game.Move, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "PASS" {
		return game.Move{X: -1, Y: -1}, nil
	}
	if len(value) >= 2 && len(value) <= 3 {
		x := strings.IndexByte(boardColumns, value[0])
		row, err := strconv.Atoi(value[1:])
		if err == nil && x >= 0 && x < size && row >= 1 && row <= size && value[1:] == strconv.Itoa(row) {
			return game.Move{X: x, Y: size - row}, nil
		}
	}
	return game.Move{}, errors.New("请使用 D4 这样的坐标（跳过 I 列）、pass 或 resign。")
}

func randomTableToken(length int) (string, error) {
	value := make([]byte, length)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func tableLocalRequest(w http.ResponseWriter, r *http.Request) bool {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(peer).IsLoopback() {
		writeError(w, http.StatusForbidden, "请在本机连接棋桌。")
		return false
	}
	host := r.Host
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	host = strings.Trim(host, "[]")
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		writeError(w, http.StatusForbidden, "棋桌接口仅接受 localhost 或回环 IP。")
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != scheme || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			writeError(w, http.StatusForbidden, "请从同源应用页面发起请求。")
			return false
		}
	}
	return true
}

func decodeTableRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	if !tableLocalRequest(w, r) {
		return false
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "请求须使用 application/json。")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确或超过 4 KiB。")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "请求只能包含一个 JSON 对象。")
		return false
	}
	return true
}
