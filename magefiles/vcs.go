//go:build mage
// +build mage

package main

import (
	"context"
	"strings"
)

func commitSha(ctx context.Context, dir string) string {
	op := NewCmdOpts()
	opts := []NewCmdOption{
		op.Fields("git log -n 1 --pretty=format:%H"),
		op.CaptureOut(true),
	}

	if dir != "" && dir != "." {
		opts = append(opts, op.Dir(dir))
	}

	cmd := NewCmd(opts...)

	if err := cmd.Run(ctx); err != nil {
		panic(err)
	}

	return cmd.OutString(strings.TrimSpace)
}
