//go:build mage
// +build mage

package main

import (
	"context"
	"strings"
)

func commitSha(ctx context.Context, dir string) string {
	cmd := NewCmd().
		Fields("git log -n 1 --pretty=format:%H").
		CaptureOut()

	if dir != "" && dir != "." {
		cmd.Dir(dir)
	}

	if err := cmd.Run(ctx); err != nil {
		panic(err)
	}

	return cmd.OutString(strings.TrimSpace)
}
