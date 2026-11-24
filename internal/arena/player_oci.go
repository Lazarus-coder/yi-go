package arena

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Image arguments cannot add mounts, credentials, networking or privileges.
func containerArgs(config DriverConfig, name string) []string {
	memory, cpus := config.MemoryMB, config.CPUs
	if memory == 0 {
		memory = 256
	}
	if cpus == 0 {
		cpus = 1
	}
	limit := strconv.Itoa(memory) + "m"
	args := []string{"create", "--name", name, "--pull=never", "--interactive",
		"--network=none", "--read-only", "--user=65534:65534", "--cap-drop=ALL",
		"--security-opt=no-new-privileges", "--pids-limit=64", "--ipc=none",
		"--memory=" + limit, "--memory-swap=" + limit, "--cpus=" + strconv.FormatFloat(cpus, 'f', -1, 64),
		"--tmpfs=/tmp:rw,noexec,nosuid,nodev,size=16m", "--no-healthcheck", "--log-driver=none",
		config.Image}
	return append(args, config.Command...)
}

func (p *processPlayer) createContainer(ctx context.Context, config DriverConfig) error {
	p.runtime = config.Runtime
	if p.runtime == "" {
		p.runtime = "docker"
	}
	// Reject image-declared anonymous volumes: they would bypass the read-only
	// root and create writable host-backed storage outside the bounded tmpfs.
	image, err := runtimeOutput(ctx, p.runtime, "image", "inspect", config.Image)
	if err != nil {
		return fmt.Errorf("OCI image/daemon unavailable: %w", err)
	}
	var images []struct {
		Config struct{ Volumes map[string]json.RawMessage }
	}
	if json.Unmarshal(image, &images) != nil || len(images) != 1 || len(images[0].Config.Volumes) != 0 {
		return errors.New("OCI image must exist locally and must not declare VOLUME mounts")
	}
	p.container = "yi-agent-" + p.session
	if _, err := runtimeOutput(ctx, p.runtime, containerArgs(config, p.container)...); err != nil {
		p.removeContainer()
		return fmt.Errorf("OCI container creation failed: %w", err)
	}
	return nil
}

// A runtime/entrypoint failure is host configuration, not an agent's chess loss.
func (p *processPlayer) containerStartupError(original error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data, err := runtimeOutput(ctx, p.runtime, "inspect", p.container)
	if err != nil {
		return &StartupError{fmt.Errorf("inspect failed agent container: %w", err)}
	}
	var states []struct {
		State struct{ Error, StartedAt string }
	}
	if json.Unmarshal(data, &states) != nil || len(states) != 1 {
		return &StartupError{errors.New("invalid OCI container state")}
	}
	state := states[0].State
	if state.Error != "" || state.StartedAt == "" || strings.HasPrefix(state.StartedAt, "0001-") {
		return &StartupError{errors.New("OCI runtime could not start the configured entrypoint")}
	}
	return original
}

func (p *processPlayer) removeContainer() error {
	if p.container == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := runtimeOutput(ctx, p.runtime, "rm", "--force", "--volumes", p.container)
	return err
}

type boundedOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	n := len(data)
	if n > maxAgentLine-b.Len() {
		b.overflow = true
		data = data[:maxAgentLine-b.Len()]
	}
	b.Buffer.Write(data)
	return n, nil
}

func runtimeOutput(ctx context.Context, runtime string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(runtime, args...)
	configureProcess(cmd)
	cmd.WaitDelay = time.Second
	var stdout, stderr boundedOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { killProcess(cmd) })
	err := cmd.Wait()
	stop()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("%s %s failed: %w", runtime, args[0], err)
	}
	if stdout.overflow {
		return nil, errors.New("OCI metadata exceeds 64 KiB")
	}
	return stdout.Bytes(), nil
}
