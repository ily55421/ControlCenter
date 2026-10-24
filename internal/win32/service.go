// Package win32 — Windows 服务查询 / 安装 / 卸载(SCM)。
package win32

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// ServiceExists 服务是否存在。
func ServiceExists(name string) bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return false
	}
	s.Close()
	return true
}

// ServiceRunning 服务是否运行中。
func ServiceRunning(name string) bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return false
	}
	defer s.Close()
	st, err := s.Query()
	if err != nil {
		return false
	}
	return st.State == svc.Running
}

// StartService 启动服务并等待。
func StartService(name string, timeout time.Duration) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("服务 %s 未安装", name)
	}
	defer s.Close()
	st, err := s.Query()
	if err != nil {
		return err
	}
	if st.State == svc.Running {
		return nil
	}
	if err := s.Start(); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, err := s.Query()
		if err != nil {
			return err
		}
		if st.State == svc.Running {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("服务 %s 启动超时", name)
}

// InstallService 安装服务(自动启动)。args 会附加到 BinaryPathName 之后。
func InstallService(name, displayName, exePath string, args ...string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err == nil {
		s.Close()
		return fmt.Errorf("服务 %s 已存在", name)
	}
	binPath := `"` + exePath + `"`
	for _, a := range args {
		binPath += " " + a
	}
	s, err = m.CreateService(name, binPath, mgr.Config{
		DisplayName:  displayName,
		StartType:    windows.SERVICE_AUTO_START,
		Description:  "Control Center Hotkey Service (Go)",
		Dependencies: []string{},
	})
	if err != nil {
		return err
	}
	defer s.Close()
	return nil
}

// RemoveService 停止并卸载服务。
func RemoveService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("服务 %s 未安装", name)
	}
	defer s.Close()
	st, err := s.Query()
	if err == nil && st.State != svc.Stopped {
		_, _ = s.Control(1) // SERVICE_CONTROL_STOP
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			st, err = s.Query()
			if err != nil || st.State == svc.Stopped {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
	return s.Delete()
}

// IsServiceContext 当前进程是否运行在服务上下文(SCM 启动)。
func IsServiceContext() bool {
	p, err := svc.IsWindowsService()
	return err == nil && p
}

var _ = unsafe.Pointer(nil)
