//go:build !windows

package main

import (
	"io"

	"openknowledge/internal/daemon"
)

func runHost(stdout, stderr io.Writer) int { return daemon.OpenGUI(stdout, stderr) }
