package internal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func Run(command string, arguments ...string) error {
	_, err := RunOutput(command, arguments...)
	return err
}

func RunOutput(command string, args ...string) (out string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	var stdout, stderr bytes.Buffer
	defer func() {
		cancel()
		if err != nil {
			err = fmt.Errorf("%s %s: %w: %s", command, strings.Join(args, " "), err, stderr.String())
		}
	}()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// A daemon started by the command can hold the pipes open after it exits (upstream #156).
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	out = strings.TrimSpace(stdout.String())
	return
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	for ; err != nil; err = errors.Unwrap(err) {
		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus()
			}
		}
	}
	return -1
}
