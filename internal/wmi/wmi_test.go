package wmi

import "testing"

// TestEncodeSetWMI 验证 SetWMI 子命令编码:data 低 3 字节 + 子命令覆盖 byte[3],256 字节缓冲。
func TestEncodeSetWMI(t *testing.T) {
	buf := encodeSetWMI(25, 2)
	if len(buf) != 256 {
		t.Fatalf("缓冲长度 = %d, 期望 256", len(buf))
	}
	if buf[0] != 2 || buf[1] != 0 || buf[2] != 0 {
		t.Errorf("data 低 3 字节 = %v, 期望 [2 0 0]", buf[0:3])
	}
	if buf[3] != 25 {
		t.Errorf("byte[3] = %d, 期望子命令 25", buf[3])
	}
	for i := 4; i < 256; i++ {
		if buf[i] != 0 {
			t.Fatalf("buf[%d] = %d, 期望 0", i, buf[i])
		}
	}
}

// TestEncodeSetWMIHighByteOverwritten data 高字节被子命令覆盖(与原生行为一致)。
func TestEncodeSetWMIHighByteOverwritten(t *testing.T) {
	buf := encodeSetWMI(0x1F, 0xAABBCCDD)
	if buf[0] != 0xDD || buf[1] != 0xCC || buf[2] != 0xBB {
		t.Errorf("低 3 字节 = %v, 期望 [0xDD 0xCC 0xBB]", buf[0:3])
	}
	if buf[3] != 0x1F {
		t.Errorf("byte[3] = %#x, 期望子命令 0x1F", buf[3])
	}
}
