package arena

import (
	"context"
	"os/exec"
	"strconv"
	"time"
)

func configureProcess(cmd *exec.Cmd) {}

func killProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	cmd.Process.Kill()
}
