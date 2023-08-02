//go:build mage
// +build mage

package main

import (
	"strings"
)

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
