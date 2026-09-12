//go:build !windows

package chatsc

import "os/exec"

func hideWindow(*exec.Cmd) {}
