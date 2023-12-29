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

type newCmdOpts struct{}

func NewCmdOpts() newCmdOpts {
	return newCmdOpts{}
}

type sharedCmdConfig struct {
	envPre          []string
	envPost         []string
	cmdAndArgs      []string
	stdin           io.Reader
	dir             string
	ignoreParentEnv bool
	echoDisabled    bool
}

type newCmdConfig struct {
	sharedCmdConfig
	bufOut, bufErr bool
}

type NewCmdOption func(*newCmdConfig)

func NewCmd(options ...NewCmdOption) *Cmd {
	cfg := newCmdOpts{}.newCmdConfig(options...)

	var bufOut, bufErr *bytes.Buffer

	if cfg.bufOut {
		bufOut = &bytes.Buffer{}
	}

	if cfg.bufErr {
		bufErr = &bytes.Buffer{}
	}

	return &Cmd{
		envPre:          cfg.envPre,
		envPost:         cfg.envPost,
		cmdAndArgs:      cfg.cmdAndArgs,
		stdin:           cfg.stdin,
		dir:             cfg.dir,
		ignoreParentEnv: cfg.ignoreParentEnv,
		echoDisabled:    cfg.echoDisabled,
		bufOut:          bufOut,
		bufErr:          bufErr,
	}
}

func (newCmdOpts) newCmdConfig(options ...NewCmdOption) newCmdConfig {
	cfg := newCmdConfig{}

	for _, f := range options {
		f(&cfg)
	}

	return cfg
}

func (newCmdOpts) Fields(s string) NewCmdOption {
	return func(cfg *newCmdConfig) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}

		cfg.sharedCmdConfig.args(strings.Fields(s)...)
	}
}

func (newCmdOpts) Arg(s string) NewCmdOption {
	return func(cfg *newCmdConfig) {
		cfg.sharedCmdConfig.args(s)
	}
}

func (newCmdOpts) Args(s ...string) NewCmdOption {
	return func(cfg *newCmdConfig) {
		if len(cfg.cmdAndArgs) == 0 {
			var c []string

			if n := len(s); n > 0 {
				c = make([]string, n)
				copy(c, s)
			}

			cfg.cmdAndArgs = c
			return
		}

		cfg.cmdAndArgs = append(cfg.cmdAndArgs, s...)
	}
}

func (cfg *sharedCmdConfig) args(s ...string) {
	if cfg.cmdAndArgs == nil {
		cfg.cmdAndArgs = s
		return
	}
	cfg.cmdAndArgs = append(cfg.cmdAndArgs, s...)
}

func (newCmdOpts) EchoDisabled(b bool) NewCmdOption {
	return func(cfg *newCmdConfig) {
		cfg.echoDisabled = b
	}
}

func (newCmdOpts) ReplaceArgs(s ...string) NewCmdOption {
	return func(cfg *newCmdConfig) {
		cmd := cfg.cmdAndArgs[0]

		if n := len(s); n > 0 {
			c := make([]string, n+1)
			c[0] = cmd
			copy(c[1:], s)
			cfg.cmdAndArgs = c
			return
		}

		if len(cfg.cmdAndArgs) == 1 {
			return
		}

		cfg.cmdAndArgs = []string{cmd}
	}
}

func (newCmdOpts) ReplaceCmdAndArgs(s ...string) NewCmdOption {
	return func(cfg *newCmdConfig) {
		_ = s[0] // intentionally trigger a panic if arguments are bad

		c := make([]string, len(s))
		copy(c, s)

		cfg.cmdAndArgs = c
	}
}

func (newCmdOpts) Capture(b bool) NewCmdOption {
	return func(cfg *newCmdConfig) {
		cfg.bufOut = b
		cfg.bufErr = b
	}
}

func (newCmdOpts) CaptureOut(b bool) NewCmdOption {
	return func(cfg *newCmdConfig) {
		cfg.bufOut = b
	}
}

func (newCmdOpts) CaptureErr(b bool) NewCmdOption {
	return func(cfg *newCmdConfig) {
		cfg.bufErr = b
	}
}

func (newCmdOpts) Dir(s string) NewCmdOption {
	return func(cfg *newCmdConfig) {
		cfg.dir = s
	}
}

func (newCmdOpts) Stdin(r io.Reader) NewCmdOption {
	return func(cfg *newCmdConfig) {
		cfg.stdin = r
	}
}

func (newCmdOpts) IgnoreParentEnv(b bool) NewCmdOption {
	return func(cfg *newCmdConfig) {
		cfg.ignoreParentEnv = b
	}
}

func (newCmdOpts) Cmd(s string) NewCmdOption {
	return func(cfg *newCmdConfig) {
		if len(cfg.cmdAndArgs) <= 1 {
			cfg.cmdAndArgs = []string{s}
			return
		}

		cfg.cmdAndArgs = append([]string{s}, cfg.cmdAndArgs[1:]...)
	}
}

func (newCmdOpts) PrependEnvMap(m map[string]string) NewCmdOption {
	return func(cfg *newCmdConfig) {
		env := make([]string, 0, len(m))
		for k, v := range m {
			env = append(env, k+"="+v)
		}

		op := newCmdOpts{}.PrependEnv(env...)
		op(cfg)
	}
}

func (newCmdOpts) PrependEnv(s ...string) NewCmdOption {
	return func(cfg *newCmdConfig) {
		cfg.envPre = append(append(make([]string, 0, len(s)+len(cfg.envPre)), s...), cfg.envPre...)
	}
}

func (newCmdOpts) AppendEnvMap(m map[string]string) NewCmdOption {
	return func(cfg *newCmdConfig) {
		env := make([]string, 0, len(m))
		for k, v := range m {
			env = append(env, k+"="+v)
		}

		op := newCmdOpts{}.AppendEnv(env...)
		op(cfg)
	}
}

func (newCmdOpts) AppendEnv(s ...string) NewCmdOption {
	return func(cfg *newCmdConfig) {
		cfg.envPost = append(append(make([]string, 0, len(cfg.envPost)+len(s)), cfg.envPost...), s...)
	}
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

func applyStrTranforms(s string, transforms ...func(string) string) string {
	for _, f := range transforms {
		s = f(s)
	}

	return s
}

func (c *Cmd) OutString(transforms ...func(string) string) string {
	if buf := c.bufOut; buf != nil {
		return applyStrTranforms(buf.String(), transforms...)
	}

	return applyStrTranforms("", transforms...)
}

func (c *Cmd) ErrString(transforms ...func(string) string) string {
	if buf := c.bufErr; buf != nil {
		return applyStrTranforms(buf.String(), transforms...)
	}

	return applyStrTranforms("", transforms...)
}
