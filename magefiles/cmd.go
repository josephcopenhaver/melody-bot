//go:build mage
// +build mage

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

type Cmd struct {
	envPre          []string
	envPost         []string
	cmdAndArgs      []string
	stdin           io.Reader
	dir             string
	bufOut, bufErr  *bytes.Buffer
	ignoreParentEnv bool
	echoDisabled    bool
}

func NewCmd(cmdAndArgs ...string) *Cmd {
	if len(cmdAndArgs) <= 0 {
		return &Cmd{}
	}

	cmd := strings.TrimSpace(cmdAndArgs[0])
	if cmd == "" {
		panic(errors.New("first cmd argument cannot be an empty string"))
	}

	newCmdAndArgs := make([]string, 0, len(cmdAndArgs))
	newCmdAndArgs = append(newCmdAndArgs, cmd)
	newCmdAndArgs = append(newCmdAndArgs, cmdAndArgs[1:]...)

	return &Cmd{
		cmdAndArgs: newCmdAndArgs,
	}
}

func (c *Cmd) Fields(s string) *Cmd {
	return c.Args(strings.Fields(strings.TrimSpace(s))...)
}

func (c *Cmd) Arg(s string) *Cmd {
	return c.Args(s)
}

func (c *Cmd) Args(s ...string) *Cmd {
	c.cmdAndArgs = append(c.cmdAndArgs, s...)

	return c
}

func (c *Cmd) EchoDisabled(b bool) *Cmd {
	c.echoDisabled = b

	return c
}

func (c *Cmd) ReplaceArgs(s ...string) *Cmd {
	if c.cmdAndArgs == nil {
		panic(errors.New("no command specified: must specify a command before attempting to ReplaceArgs"))
	}

	c.cmdAndArgs = append([]string{c.cmdAndArgs[0]}, s...)

	return c
}

func (c *Cmd) ReplaceCmdAndArgs(s ...string) *Cmd {
	if len(s) <= 0 {
		c.cmdAndArgs = nil

		return c
	}

	newList := make([]string, len(s))
	copy(newList, s)

	c.cmdAndArgs = newList

	return c
}

func (c *Cmd) Exec() error {
	var parentEnv []string
	if !c.ignoreParentEnv {
		parentEnv = os.Environ()
	}

	env := make([]string, len(c.envPre)+len(parentEnv)+len(c.envPost))

	if len(env) > 0 {
		idx := 0

		copy(env[idx:idx+len(c.envPre)], c.envPre)
		idx += len(c.envPre)

		copy(env[idx:idx+len(parentEnv)], parentEnv)
		idx += len(parentEnv)

		copy(env[idx:idx+len(c.envPost)], c.envPost)
	}

	// preflight checks
	{
		var perr error

		if c.cmdAndArgs == nil {
			perr = errors.Join(perr, errors.New("no command specified"))
		}

		if c.bufOut != nil {
			perr = errors.Join(perr, errors.New("stdout cannot be captured during an exec syscall"))
		}

		if c.bufErr != nil {
			perr = errors.Join(perr, errors.New("stderr cannot be captured during an exec syscall"))
		}

		if c.stdin != nil && c.stdin != os.Stdin {
			perr = errors.Join(perr, errors.New("stdin cannot be customized during an exec syscall"))
		}

		if perr != nil {
			return perr
		}
	}

	cmd := exec.Command(c.cmdAndArgs[0], c.cmdAndArgs[1:]...)

	if d := c.dir; d != "" && d != "." {
		if err := os.Chdir(d); err != nil {
			return err
		}
	}

	return syscall.Exec(cmd.Path, cmd.Args, env)
}

func (c *Cmd) Run(ctx context.Context) error {

	var env []string
	if len(c.envPre) > 0 || len(c.envPost) > 0 {
		var parentEnv []string
		if !c.ignoreParentEnv {
			parentEnv = os.Environ()
		}

		env = make([]string, len(c.envPre)+len(parentEnv)+len(c.envPost))

		idx := 0

		copy(env[idx:idx+len(c.envPre)], c.envPre)
		idx += len(c.envPre)

		copy(env[idx:idx+len(parentEnv)], parentEnv)
		idx += len(parentEnv)

		copy(env[idx:idx+len(c.envPost)], c.envPost)
	} else if c.ignoreParentEnv {
		env = []string{}
	}

	var cmd *exec.Cmd
	if ctx == nil {
		cmd = exec.Command(c.cmdAndArgs[0], c.cmdAndArgs[1:]...)
	} else {
		cmd = exec.CommandContext(ctx, c.cmdAndArgs[0], c.cmdAndArgs[1:]...)
	}

	if c.bufOut != nil {
		cmd.Stdout = c.bufOut
	} else {
		cmd.Stdout = os.Stdout
	}
	if c.bufErr != nil {
		cmd.Stderr = c.bufErr
	} else {
		cmd.Stderr = os.Stderr
	}
	cmd.Dir = c.dir
	if c.stdin != nil {
		cmd.Stdin = c.stdin
	}
	cmd.Env = env

	if !c.echoDisabled {
		slog.InfoContext(ctx,
			"running",
			"cmd", cmd.String(),
		)
	}

	if err := cmd.Run(); err != nil {
		logArgs := []any{"error", err}
		if c.bufOut != nil {
			logArgs = append(logArgs, "stdout", c.bufOut.String())
		}
		if c.bufErr != nil {
			logArgs = append(logArgs, "stderr", c.bufErr.String())
		}
		slog.ErrorContext(ctx,
			"command failed",
			logArgs...,
		)
		return err
	}

	return nil
}

func (c *Cmd) Dir(s string) *Cmd {
	c.dir = s
	return c
}

func (c *Cmd) Capture() *Cmd {

	if c.bufOut == nil {
		c.bufOut = &bytes.Buffer{}
	}

	return c
}

func (c *Cmd) CaptureOut() *Cmd {

	if c.bufOut == nil {
		c.bufOut = &bytes.Buffer{}
	}

	if c.bufErr == nil {
		c.bufErr = &bytes.Buffer{}
	}

	return c
}

func (c *Cmd) CaptureErr() *Cmd {

	if c.bufErr == nil {
		c.bufErr = &bytes.Buffer{}
	}

	return c
}

func applyStrFilters(s string, filters ...func(string) string) string {
	for _, f := range filters {
		s = f(s)
	}

	return s
}

func (c *Cmd) OutString(filters ...func(string) string) string {
	if buf := c.bufOut; buf != nil {
		return applyStrFilters(buf.String(), filters...)
	}

	return applyStrFilters("", filters...)
}

func (c *Cmd) ErrString(filters ...func(string) string) string {
	if buf := c.bufErr; buf != nil {
		return applyStrFilters(buf.String(), filters...)
	}

	return applyStrFilters("", filters...)
}

func (c *Cmd) Stdin(r io.Reader) *Cmd {

	c.stdin = r

	return c
}

func (c *Cmd) PrependEnvMap(m map[string]string) *Cmd {

	env := make([]string, 0, len(m))
	for k, v := range m {
		env = append(env, k+"="+v)
	}

	return c.PrependEnv(env...)
}

func (c *Cmd) PrependEnv(s ...string) *Cmd {

	c.envPre = append(append(make([]string, 0, len(s)+len(c.envPre)), s...), c.envPre...)

	return c
}

func (c *Cmd) AppendEnvMap(m map[string]string) *Cmd {

	env := make([]string, 0, len(m))
	for k, v := range m {
		env = append(env, k+"="+v)
	}

	return c.AppendEnv(env...)
}

func (c *Cmd) AppendEnv(s ...string) *Cmd {

	c.envPost = append(append(make([]string, 0, len(c.envPost)+len(s)), c.envPost...), s...)

	return c
}

func (c *Cmd) Clone() *Cmd {
	cc := *c

	if b := cc.bufOut; b != nil {
		cc.bufOut = &bytes.Buffer{}
	}

	if b := cc.bufErr; b != nil {
		cc.bufErr = &bytes.Buffer{}
	}

	if v := cc.envPre; v != nil {
		env := make([]string, len(v))
		copy(env, v)
		cc.envPre = env
	}

	if v := cc.envPost; v != nil {
		env := make([]string, len(v))
		copy(env, v)
		cc.envPost = env
	}

	if v := cc.cmdAndArgs; v != nil {
		cmdAndArgs := make([]string, len(v))
		copy(cmdAndArgs, v)
		cc.cmdAndArgs = cmdAndArgs
	}

	return &cc
}
