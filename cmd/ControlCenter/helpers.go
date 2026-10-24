// ControlCenter 启动器辅助。
package main

import (
	"os/exec"
	"strings"
)

func runCmd(args []string) error {
	cmd := exec.Command(args[0], args[1:]...)
	var sb strings.Builder
	cmd.Stdout = &sb
	cmd.Stderr = &sb
	return cmd.Run()
}
