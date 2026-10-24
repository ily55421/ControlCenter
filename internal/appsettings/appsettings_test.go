package appsettings

import "testing"

// TestNormalizePowerMode 合法值原样返回,非法值回退 Entertainment(与原版一致)。
func TestNormalizePowerMode(t *testing.T) {
	for v := 0; v <= 4; v++ {
		if got := normalizePowerMode(v); int(got) != v {
			t.Errorf("normalizePowerMode(%d) = %d", v, got)
		}
	}
	for _, v := range []int{5, 7, 100, 255} {
		if got := normalizePowerMode(v); got != Entertainment {
			t.Errorf("normalizePowerMode(%d) = %d, 期望 Entertainment(%d)", v, got, Entertainment)
		}
	}
}

// TestClevoID 两字节交换后按 Int16 解码(真机值 25990 = 0x6586)。
func TestClevoID(t *testing.T) {
	if got := clevoID([]byte{0x65, 0x86}); got != 25990 {
		t.Errorf("clevoID([0x65 0x86]) = %d, 期望 25990", got)
	}
	if got := clevoID([]byte{0x00, 0x00}); got != 0 {
		t.Errorf("clevoID([0 0]) = %d, 期望 0", got)
	}
	// 负值(b[0] 高位为 1,交换后按 int16 解释)
	if got := clevoID([]byte{0x80, 0x00}); got != -32768 {
		t.Errorf("clevoID([0x80 0x00]) = %d, 期望 -32768", got)
	}
}
