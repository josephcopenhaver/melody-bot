//go:build mage
// +build mage

package main

import (
	"context"
	"strings"
)

func commitSha(ctx context.Context, dir string) string {
	cmd := NewCmd(CmdB().Fields("git log -n 1 --pretty=format:%H").New()...).
		CaptureOut()

	if dir != "" && dir != "." {
		cmd.Dir(dir)
	}

	if err := cmd.Run(ctx); err != nil {
		panic(err)
	}

	return cmd.OutString(strings.TrimSpace)
}
