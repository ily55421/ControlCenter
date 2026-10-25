package win32

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProcessExistsOtherExcludesSelf 单实例检测必须排除自身 PID。
// 回归:早期 ProcessExistsByName 把调用者自己算作同名进程,导致 ccapp/ccfnkey
// 启动即判定"已在运行"并退出(Web UI 与托盘永远不出现)。
func TestProcessExistsOtherExcludesSelf(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("无法取得自身路径: %v", err)
	}
	name := strings.ToLower(filepath.Base(self))
	// 自身确实在进程列表里
	if !ProcessExistsByName(name) {
		t.Fatalf("ProcessExistsByName(%q) 应为 true(自身在运行)", name)
	}
	// 但排除自身后应为 false(测试进程是唯一同名进程)
	if ProcessExistsOther(name) {
		t.Errorf("ProcessExistsOther(%q) 应为 false(只应匹配自身)", name)
	}
}

// TestProcessExistsOtherUnknownName 不存在的进程名应为 false。
func TestProcessExistsOtherUnknownName(t *testing.T) {
	if ProcessExistsOther("no-such-process-cc-test.exe") {
		t.Error("不存在的进程名应为 false")
	}
}

// TestListProcesses 进程枚举必须至少返回自身。
func TestListProcesses(t *testing.T) {
	list := ListProcesses()
	if len(list) == 0 {
		t.Fatal("ListProcesses 返回空列表")
	}
	self := uint32(os.Getpid())
	for _, p := range list {
		if p.PID == self {
			return
		}
	}
	// TH32CS_SNAPPROCESS 通常包含自身;若被隐藏则只记录不失败
	t.Logf("进程列表未包含自身 PID %d(共 %d 项)", self, len(list))
}

// TestProcessesExistSingleSnapshot 多进程名一次快照判定,结果与逐个查一致。
func TestProcessesExistSingleSnapshot(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("无法取得自身路径: %v", err)
	}
	name := strings.ToLower(filepath.Base(self))
	got := ProcessesExist(name, "no-such-process-cc-test.exe")
	if !got[name] {
		t.Errorf("ProcessesExist 漏判自身 %q", name)
	}
	if got["no-such-process-cc-test.exe"] {
		t.Error("ProcessesExist 把不存在的进程判为存在")
	}
}

// TestSingleInstanceGuard 同一进程内重复申请同一互斥量,第二次必须报"已存在"。
// 这是各 exe 启动时的单实例判据(取代按 exe 名枚举进程)。
func TestSingleInstanceGuard(t *testing.T) {
	dup, err := SingleInstanceGuard("unit-test-probe")
	if err != nil {
		t.Skipf("CreateMutex 不可用: %v", err)
	}
	if dup {
		t.Skip("同名互斥量已存在(上次测试未释放),跳过")
	}
	if dup2, err := SingleInstanceGuard("unit-test-probe"); err != nil || !dup2 {
		t.Errorf("第二次申请 dup=%v err=%v, 期望 dup=true", dup2, err)
	}
}
