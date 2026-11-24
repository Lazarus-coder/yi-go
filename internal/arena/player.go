package arena

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Lazarus-coder/yi-go/internal/game"
)

const (
	FaultTimeout  = "timeout"
	FaultProtocol = "protocol"
	FaultProcess  = "process"
	protocol      = "yi-agent-v1"
	maxAgentLine  = 64 << 10
)

type Decision struct {
	Move   game.Move
	Resign bool
}

type Player interface {
	Move(context.Context, *game.Game) (Decision, error)
	Close() error
}

type Fault struct{ Code, Detail string }

func (e *Fault) Error() string { return e.Code + ": " + e.Detail }

// StartupError describes a host/setup failure, never evidence of an agent loss.
type StartupError struct{ Err error }

func (e *StartupError) Error() string { return "agent infrastructure: " + e.Err.Error() }
func (e *StartupError) Unwrap() error { return e.Err }

func FaultCode(err error) string {
	var fault *Fault
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return FaultTimeout
	case errors.As(err, &fault):
		return fault.Code
	default:
		return FaultProcess
	}
}

type DriverConfig struct {
	Mode             string   `json:"mode"`
	Command          []string `json:"command,omitempty"`
	Image            string   `json:"image,omitempty"`
	Runtime          string   `json:"runtime,omitempty"`
	StartupTimeoutMS int      `json:"startupTimeoutMS,omitempty"`
	MemoryMB         int      `json:"memoryMB,omitempty"`
	CPUs             float64  `json:"cpus,omitempty"`
}

// GameInfo contains public match identity and limits, never command arguments.
type GameInfo struct {
	ID, BlackID, WhiteID                  string
	MaxMoves, TurnTimeoutMS, OpeningPlies int
}

func ValidateDriver(c DriverConfig) error {
	if c.StartupTimeoutMS < 0 || c.StartupTimeoutMS > 60000 || c.MemoryMB < 0 || c.MemoryMB > 8192 ||
		(c.MemoryMB > 0 && c.MemoryMB < 32) || math.IsNaN(c.CPUs) || math.IsInf(c.CPUs, 0) || c.CPUs < 0 || c.CPUs > 64 {
		return errors.New("invalid agent resource or startup limit")
	}
	if c.Mode == "command" {
		if len(c.Command) == 0 || c.Command[0] == "" || c.Image != "" || c.Runtime != "" || c.MemoryMB != 0 || c.CPUs != 0 {
			return errors.New("command mode needs argv and cannot claim OCI isolation or resource limits")
		}
	} else if c.Mode == "oci" {
		if c.Image == "" || strings.HasPrefix(c.Image, "-") || strings.ContainsAny(c.Image, " \t\r\n\x00") || (c.Runtime != "" && c.Runtime != "docker" && c.Runtime != "podman") {
			return errors.New("OCI mode needs a local image and docker or podman runtime")
		}
	} else {
		return errors.New("agent mode must explicitly be command or oci")
	}
	for _, arg := range c.Command {
		if strings.ContainsRune(arg, 0) {
			return errors.New("agent argv contains NUL")
		}
	}
	return nil
}

type processPlayer struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	reader      *bufio.Reader
	done        chan struct{}
	closed      chan struct{}
	closeOnce   sync.Once
	requestMu   sync.Mutex
	closeErr    error
	runtime     string
	container   string
	session     string
	sequence    int
	size, color int
	info        GameInfo
}

func NewPlayer(ctx context.Context, config DriverConfig, size, color int, details ...GameInfo) (Player, error) {
	if err := ValidateDriver(config); err != nil {
		return nil, &StartupError{err}
	}
	if (size != 9 && size != 13 && size != 19) || (color != game.Black && color != game.White) {
		return nil, &StartupError{errors.New("invalid board size or player color")}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, &StartupError{err}
	}
	p := &processPlayer{session: hex.EncodeToString(token), size: size, color: color, done: make(chan struct{}), closed: make(chan struct{})}
	p.info = GameInfo{ID: p.session, BlackID: "black", WhiteID: "white", MaxMoves: 2000, TurnTimeoutMS: 30000}
	if len(details) > 1 {
		return nil, &StartupError{errors.New("expected one game identity")}
	}
	if len(details) == 1 {
		p.info = details[0]
	}
	if p.info.ID == "" || p.info.BlackID == "" || p.info.WhiteID == "" || p.info.MaxMoves < 2 || p.info.MaxMoves > 2000 ||
		p.info.TurnTimeoutMS < 1 || p.info.TurnTimeoutMS > 120000 || p.info.OpeningPlies < 0 || p.info.OpeningPlies >= p.info.MaxMoves {
		return nil, &StartupError{errors.New("invalid public game identity or limits")}
	}
	startupMS := config.StartupTimeoutMS
	if startupMS == 0 {
		startupMS = 5000
	}
	startup, cancel := context.WithTimeout(ctx, time.Duration(startupMS)*time.Millisecond)
	defer cancel()
	argv := config.Command
	if config.Mode == "oci" {
		if err := p.createContainer(startup, config); err != nil {
			return nil, &StartupError{err}
		}
		argv = []string{p.runtime, "start", "--attach", "--interactive", p.container}
	}
	p.cmd = exec.Command(argv[0], argv[1:]...)
	p.cmd.WaitDelay = time.Second
	configureProcess(p.cmd)
	p.cmd.Stderr = io.Discard // Agent diagnostics may contain provider credentials.
	var err error
	if p.stdin, err = p.cmd.StdinPipe(); err == nil {
		p.stdout, err = p.cmd.StdoutPipe()
	}
	if err == nil {
		err = p.cmd.Start()
	}
	if err != nil {
		if p.stdin != nil {
			p.stdin.Close()
		}
		if p.stdout != nil {
			p.stdout.Close()
		}
		p.removeContainer()
		return nil, &StartupError{fmt.Errorf("start agent: %w", err)}
	}
	p.reader = bufio.NewReaderSize(p.stdout, maxAgentLine+1)
	go func() { p.cmd.Wait(); close(p.done) }()
	go func() {
		select {
		case <-ctx.Done():
			p.Close()
		case <-p.closed:
		}
	}()
	id := p.session + ":hello"
	line, err := p.exchange(startup, map[string]any{
		"type": "hello", "protocol": protocol, "id": id, "gameId": p.info.ID, "color": game.ColorName(p.color), "size": size, "komi": game.Komi,
		"players":  map[string]string{"black": p.info.BlackID, "white": p.info.WhiteID},
		"maxMoves": p.info.MaxMoves, "turnTimeoutMS": p.info.TurnTimeoutMS,
		"revision": p.info.OpeningPlies, "remainingMoves": p.info.MaxMoves - p.info.OpeningPlies,
	})
	if err == nil {
		var ready struct{ Type, Protocol, ID string }
		err = decodeLine(line, &ready, "type", "protocol", "id")
		if err == nil && (ready.Type != "ready" || ready.Protocol != protocol || ready.ID != id) {
			err = &Fault{FaultProtocol, "ready must match the hello id and protocol"}
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		} else if p.container != "" {
			err = p.containerStartupError(err)
		}
		if cleanup := p.Close(); cleanup != nil {
			err = cleanup
		}
		return nil, err
	}
	return p, nil
}

func (p *processPlayer) Move(ctx context.Context, g *game.Game) (Decision, error) {
	p.requestMu.Lock()
	defer p.requestMu.Unlock()
	if g.Size != p.size || g.Next != p.color || g.Over || len(g.Moves) >= p.info.MaxMoves {
		return Decision{}, &StartupError{errors.New("referee supplied a position for the wrong player")}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.info.TurnTimeoutMS)*time.Millisecond)
	defer cancel()
	deadline, _ := ctx.Deadline()
	p.sequence++
	id := p.session + ":" + strconv.Itoa(p.sequence)
	observation := game.Observe(g, p.info.MaxMoves)
	observation["type"] = "turn"
	observation["protocol"] = protocol
	observation["id"] = id
	observation["gameId"] = p.info.ID
	observation["players"] = map[string]string{"black": p.info.BlackID, "white": p.info.WhiteID}
	observation["turnTimeoutMS"] = p.info.TurnTimeoutMS
	observation["timeLimitMS"] = max(1, time.Until(deadline).Milliseconds())
	line, err := p.exchange(ctx, observation)
	var reply struct {
		Type     string `json:"type"`
		ID       string `json:"id"`
		Revision *int   `json:"revision"`
		Move     string `json:"move"`
	}
	if err == nil {
		err = decodeLine(line, &reply, "type", "id", "revision", "move")
	}
	if err == nil && (reply.Type != "move" || reply.ID != id || reply.Revision == nil || *reply.Revision != len(g.Moves)) {
		err = &Fault{FaultProtocol, "move must match the current request id and revision"}
	}
	decision := Decision{}
	if err == nil {
		if strings.EqualFold(strings.TrimSpace(reply.Move), "resign") {
			decision.Resign = true
		} else {
			decision.Move, err = game.ParseMove(reply.Move, g.Size)
			if err != nil {
				err = &Fault{FaultProtocol, "invalid move notation"}
			}
		}
	}
	if err != nil {
		if cleanup := p.Close(); cleanup != nil {
			return Decision{}, cleanup
		}
	}
	return decision, err
}

func (p *processPlayer) exchange(ctx context.Context, message any) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(message)
	if err != nil || len(data) >= maxAgentLine {
		return nil, &StartupError{errors.New("observation exceeds protocol limit")}
	}
	type response struct {
		line []byte
		err  error
	}
	result := make(chan response, 1)
	go func() {
		if p.reader.Buffered() > 0 {
			result <- response{err: &Fault{FaultProtocol, "unsolicited extra stdout"}}
			return
		}
		if _, err := p.stdin.Write(append(data, '\n')); err != nil {
			result <- response{err: &Fault{FaultProcess, "agent closed stdin"}}
			return
		}
		line, err := p.reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) || len(line) > maxAgentLine {
			err = &Fault{FaultProtocol, "response exceeds 64 KiB"}
		} else if err != nil {
			err = &Fault{FaultProcess, "agent exited or closed stdout"}
		} else if p.reader.Buffered() > 0 {
			err = &Fault{FaultProtocol, "expected exactly one response line"}
		}
		result <- response{line: append([]byte(nil), line...), err: err}
	}()
	select {
	case <-ctx.Done():
		killProcess(p.cmd)
		p.stdout.Close()
		p.stdin.Close()
		<-result
		return nil, ctx.Err()
	case <-p.closed:
		<-result
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Fault{FaultProcess, "agent has stopped"}
	case reply := <-result:
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return reply.line, reply.err
	}
}

// Reject duplicate/unknown/missing keys as well as multiple JSON values.
func decodeLine(line []byte, output any, keys ...string) error {
	invalid := &Fault{FaultProtocol, "response must be one exact JSON protocol object"}
	if !utf8.Valid(line) {
		return invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return invalid
	}
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = false
	}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		seen, known := allowed[key]
		if err != nil || !ok || !known || seen {
			return invalid
		}
		allowed[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return invalid
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return invalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return invalid
	}
	for _, seen := range allowed {
		if !seen {
			return invalid
		}
	}
	if json.Unmarshal(line, output) != nil {
		return invalid
	}
	return nil
}

func (p *processPlayer) Close() error {
	p.closeOnce.Do(func() {
		p.stdin.Close()
		select {
		case <-p.done:
		case <-time.After(100 * time.Millisecond):
		}
		killProcess(p.cmd) // Kill surviving children even if their parent exited.
		p.stdout.Close()
		<-p.done
		if err := p.removeContainer(); err != nil {
			p.closeErr = &StartupError{err}
		}
		close(p.closed)
	})
	return p.closeErr
}
