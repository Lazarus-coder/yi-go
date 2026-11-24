package arena

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Lazarus-coder/yi-go/internal/game"
)

func helperDriver(t *testing.T, mode string, args ...string) DriverConfig {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return DriverConfig{Mode: "command", Command: append([]string{exe, "-test.run=^TestAgentProcess$", "--", mode}, args...), StartupTimeoutMS: 1000}
}

// A real child process, not a mocked transport. os.Exit avoids test-runner stdout.
func TestAgentProcess(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 || len(os.Args) <= separator+1 {
		return
	}
	mode := os.Args[separator+1]
	if mode == "crash" {
		os.Exit(3)
	}
	if mode == "sleeper" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), maxAgentLine)
	turns := 0
	for scanner.Scan() {
		var request struct {
			Type, ID, GameID, Color                                        string
			Revision, MaxMoves, RemainingMoves, TurnTimeoutMS, TimeLimitMS int
			Players                                                        map[string]string
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(4)
		}
		if mode == "observation" {
			if request.GameID != "fixture-game" || request.Players["black"] != "alpha" || request.Players["white"] != "beta" ||
				request.MaxMoves != 10 || request.RemainingMoves != 10-request.Revision || request.TurnTimeoutMS != 2000 ||
				(request.Color != "black" && request.Color != "white") || request.ID == request.GameID {
				os.Exit(6)
			}
			if request.Type == "hello" && request.Revision != 2 {
				os.Exit(7)
			}
			if request.Type == "turn" && (request.TimeLimitMS < 1 || request.TimeLimitMS > 500) {
				os.Exit(8)
			}
		}
		if request.Type == "hello" {
			if mode == "silent" {
				time.Sleep(time.Minute)
			}
			json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "ready", "protocol": protocol, "id": request.ID})
			continue
		}
		turns++
		if mode == "wait" {
			time.Sleep(time.Minute)
		}
		if mode == "child" {
			child := exec.Command(os.Args[0], "-test.run=^TestAgentProcess$", "--", "sleeper")
			if child.Start() != nil {
				os.Exit(5)
			}
			os.WriteFile(os.Args[separator+2], []byte(strconv.Itoa(child.Process.Pid)), 0600)
			time.Sleep(time.Minute)
		}
		if mode == "huge" {
			fmt.Fprintln(os.Stdout, strings.Repeat("x", maxAgentLine+1))
			continue
		}
		id, move := request.ID, "pass"
		if mode == "wrong-id" {
			id = "previous-request"
		}
		if mode == "memory" {
			move = fmt.Sprintf("%c1", 'A'+turns-1)
		}
		var revision any = request.Revision
		if mode == "null-revision" {
			revision = nil
		}
		response, _ := json.Marshal(map[string]any{"type": "move", "id": id, "revision": revision, "move": move})
		response = append(response, '\n')
		if mode == "duplicate" {
			response = append(response, response...)
		}
		os.Stdout.Write(response)
	}
	os.Exit(0)
}

func TestPublicGameIdentityAndRemainingBudget(t *testing.T) {
	info := GameInfo{ID: "fixture-game", BlackID: "alpha", WhiteID: "beta", MaxMoves: 10, TurnTimeoutMS: 2000, OpeningPlies: 2}
	g, _ := game.New(9)
	g.Play(game.Move{X: 0, Y: 0})
	g.Play(game.Move{X: 2, Y: 0})
	var sessions []string
	for color := game.Black; color <= game.White; color++ {
		p, err := NewPlayer(context.Background(), helperDriver(t, "observation"), 9, color, info)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		sessions = append(sessions, p.(*processPlayer).session)
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		decision, err := p.Move(ctx, g)
		cancel()
		if err != nil {
			t.Fatalf("observation identifiers/limits mismatch: %v", err)
		}
		if err := g.Play(decision.Move); err != nil {
			t.Fatal(err)
		}
	}
	if sessions[0] == sessions[1] {
		t.Fatal("seat correlation sessions must remain distinct")
	}
}

func TestPersistentAgentAndProtocolFaults(t *testing.T) {
	g, _ := game.New(9)
	p, err := NewPlayer(context.Background(), helperDriver(t, "memory"), 9, game.Black)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, expected := range []game.Move{{X: 0, Y: 8}, {X: 1, Y: 8}} {
		decision, err := p.Move(context.Background(), g)
		if err != nil || decision.Resign || decision.Move != expected {
			t.Fatalf("memory was lost: %+v %v", decision, err)
		}
		if g.Play(decision.Move) != nil || g.Play(game.Move{X: -1, Y: -1}) != nil {
			t.Fatal("fixture could not advance")
		}
	}
	for _, mode := range []string{"wrong-id", "duplicate", "huge", "null-revision"} {
		t.Run(mode, func(t *testing.T) {
			player, err := NewPlayer(context.Background(), helperDriver(t, mode), 9, game.Black)
			if err != nil {
				t.Fatal(err)
			}
			defer player.Close()
			position, _ := game.New(9)
			_, err = player.Move(context.Background(), position)
			if FaultCode(err) != FaultProtocol {
				t.Fatalf("wanted protocol failure, got %v", err)
			}
		})
	}
}

func TestAgentStartupAndTimeoutClassification(t *testing.T) {
	_, err := NewPlayer(context.Background(), DriverConfig{Mode: "command", Command: []string{filepath.Join(t.TempDir(), "missing-agent")}}, 9, game.Black)
	var startup *StartupError
	if !errors.As(err, &startup) {
		t.Fatalf("missing executable must not produce a scored loss: %v", err)
	}
	_, err = NewPlayer(context.Background(), helperDriver(t, "crash"), 9, game.Black)
	if errors.As(err, &startup) || FaultCode(err) != FaultProcess {
		t.Fatalf("agent crash misclassified: %v", err)
	}
	p, err := NewPlayer(context.Background(), helperDriver(t, "wait"), 9, game.Black)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	g, _ := game.New(9)
	start := time.Now()
	_, err = p.Move(ctx, g)
	if FaultCode(err) != FaultTimeout || time.Since(start) > time.Second {
		t.Fatalf("unbounded/incorrect timeout: %v", err)
	}
}

func TestCancellationKillsAgentChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process-group check")
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	p, err := NewPlayer(context.Background(), helperDriver(t, "child", pidFile), 9, game.Black)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithCancel(context.Background())
	g, _ := game.New(9)
	finished := make(chan error, 1)
	go func() { _, err := p.Move(ctx, g); finished <- err }()
	var pid []byte
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pid, _ = os.ReadFile(pidFile)
		if len(pid) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(pid) == 0 {
		cancel()
		t.Fatal("child did not start")
	}
	childPID, _ := strconv.Atoi(string(pid))
	child, _ := os.FindProcess(childPID)
	defer child.Kill()
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("operator cancellation became an agent loss: %v", err)
	}
	for time.Now().Before(deadline) {
		state, _ := exec.Command("ps", "-o", "stat=", "-p", string(pid)).Output()
		if len(strings.TrimSpace(string(state))) == 0 || strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent child survived cancellation")
}

func TestExactResponseJSON(t *testing.T) {
	for _, line := range []string{
		`{"type":"move","id":"x","revision":0,"move":"pass","extra":true}`,
		`{"type":"move","id":"x","id":"x","revision":0,"move":"pass"}`,
		`{"type":"move","id":"x","move":"pass"}`,
		`{"type":"move","id":"x","revision":0,"move":"pass"} {}`,
	} {
		var reply map[string]any
		if decodeLine([]byte(line), &reply, "type", "id", "revision", "move") == nil {
			t.Fatalf("accepted malformed response: %s", line)
		}
	}
}

func TestTwoRealPythonAgents(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python 3 is not installed")
	}
	script, _ := filepath.Abs("../../examples/agent/agent.py")
	players := make([]Player, 2)
	for i := range players {
		players[i], err = NewPlayer(context.Background(), DriverConfig{Mode: "command", Command: []string{python, script, strconv.Itoa(i + 1)}}, 9, i+1)
		if err != nil {
			t.Fatal(err)
		}
		defer players[i].Close()
	}
	g, _ := game.New(9)
	for len(g.Moves) < 200 && !g.Over {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		decision, err := players[g.Next-1].Move(ctx, g)
		cancel()
		if err != nil || decision.Resign || !g.IsLegal(decision.Move) {
			t.Fatalf("external agent failed: %+v %v", decision, err)
		}
		if err := g.Play(decision.Move); err != nil {
			t.Fatal(err)
		}
	}
	if !g.Over {
		t.Fatal("real agents did not finish their game")
	}
	t.Logf("two external Python processes completed %d legal moves", len(g.Moves))
}

func TestOCIIsolationArguments(t *testing.T) {
	args := containerArgs(DriverConfig{Mode: "oci", Image: "agent:local"}, "test-agent")
	for _, required := range []string{"--pull=never", "--network=none", "--read-only", "--user=65534:65534", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=64", "--memory=256m", "--memory-swap=256m", "--cpus=1", "--tmpfs=/tmp:rw,noexec,nosuid,nodev,size=16m"} {
		if !slices.Contains(args, required) {
			t.Fatalf("missing isolation control %s", required)
		}
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--env") || strings.HasPrefix(arg, "--mount") || strings.HasPrefix(arg, "--volume") || strings.HasPrefix(arg, "--privileged") {
			t.Fatalf("unexpected host access %s", arg)
		}
	}
	if ValidateDriver(DriverConfig{Mode: "command", Command: []string{"python3"}, MemoryMB: 256}) == nil {
		t.Fatal("trusted command must not claim unenforced memory bounds")
	}
}

// Opt-in integration: docker build -t yi-agent:local examples/agent
// YI_TEST_OCI_IMAGE=yi-agent:local go test ./internal/arena -run TestOCIPlayer -v
func TestOCIPlayer(t *testing.T) {
	image := os.Getenv("YI_TEST_OCI_IMAGE")
	if image == "" {
		t.Skip("set YI_TEST_OCI_IMAGE to test an already running OCI runtime")
	}
	t.Setenv("YI_HOST_SENTINEL", "must-not-enter-container")
	p, err := NewPlayer(context.Background(), DriverConfig{Mode: "oci", Runtime: os.Getenv("YI_TEST_OCI_RUNTIME"), Image: image, Command: []string{"--verify-isolation", "7"}, StartupTimeoutMS: 10000}, 9, game.Black)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := game.New(9)
	decision, err := p.Move(context.Background(), g)
	if err != nil || !g.IsLegal(decision.Move) {
		t.Fatalf("isolated process failed: %+v %v", decision, err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}
