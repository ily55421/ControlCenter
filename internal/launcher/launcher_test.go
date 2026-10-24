package launcher

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteReadPort 端口文件读写往返。
func TestWriteReadPort(t *testing.T) {
	if err := os.MkdirAll(filepath.Dir(registryPortFile()), 0o755); err != nil {
		t.Skipf("无法创建目录: %v", err)
	}
	old := readPort()
	t.Cleanup(func() { WritePort(old) })

	WritePort(12345)
	if got := readPort(); got != 12345 {
		t.Errorf("readPort() = %d, 期望 12345", got)
	}
}

// TestResolvePortUsesRecorded 端口文件指向存活服务时,resolvePort 直接复用它
// (不得另起服务——这是 ccfan/cckeyboard 等面板启动器与 ccapp 共用同一 UI 的前提)。
func TestResolvePortUsesRecorded(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法监听: %v", err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	port := ln.Addr().(*net.TCPAddr).Port
	if err := os.MkdirAll(filepath.Dir(registryPortFile()), 0o755); err == nil {
		old := readPort()
		t.Cleanup(func() { WritePort(old) })
	}
	WritePort(port)
	if got := resolvePort(); got != port {
		t.Errorf("resolvePort() = %d, 期望复用 %d", got, port)
	}
}

// TestAliveDeadPort 未监听端口应为 false。
func TestAliveDeadPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法监听: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // 立即释放
	if alive(port) {
		t.Errorf("alive(%d) 应为 false", port)
	}
}
