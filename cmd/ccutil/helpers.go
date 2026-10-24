// ccutil 辅助函数。
package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

func run(args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	var sb strings.Builder
	cmd.Stdout = &sb
	cmd.Stderr = &sb
	err := cmd.Run()
	return sb.String(), err
}

// deleteRegTree 递归删除注册表键。
func deleteRegTree(root, path string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ)
	if err != nil {
		return err
	}
	subkeys, _ := k.ReadSubKeyNames(-1)
	k.Close()
	for _, sk := range subkeys {
		_ = deleteRegTree(root, path+`\`+sk)
	}
	return registry.DeleteKey(registry.CURRENT_USER, path)
}

func sleepMs(ms int) {
	d := time.Duration(ms) * time.Millisecond
	if d > time.Minute {
		// 长等待分片,避免精度问题
		for ms > 0 {
			step := 60000
			if ms < step {
				step = ms
			}
			time.Sleep(time.Duration(step) * time.Millisecond)
			ms -= step
		}
		return
	}
	time.Sleep(d)
}

var _ = fmt.Sprintf
