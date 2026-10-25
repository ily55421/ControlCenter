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

// TestEncodeScheduleValue 节能调度起始小时必须左移 6 位。
// 回归:曾写成 `start&0x3F`(未移位),于是 22:00 起被下发成约 0.34 点,
// 整个充放电时段提前到凌晨。判据取自原版 EnergySave MainWindow.cs:1910
// `num + (start<<6) + (end+1<<17)`。
func TestEncodeScheduleValue(t *testing.T) {
	cases := []struct {
		name           string
		slot, start, end int
		enabled        bool
		want           uint32
	}{
		{"槽1 22→23 点", 1, 22, 23, true, 0x03000000 | (22 << 6) | (24 << 17)},
		{"槽1 0→23 点(全天)", 1, 0, 23, true, 0x03000000 | (24 << 17)},
		{"槽2 8→12 点", 2, 8, 12, true, 0x04000000 | (8 << 6) | (13 << 17)},
		{"停用只发基址", 1, 22, 23, false, 0x03000000},
		{"槽2 停用只发基址", 2, 1, 2, false, 0x04000000},
	}
	for _, c := range cases {
		if got := encodeScheduleValue(c.slot, c.start, c.end, c.enabled); got != c.want {
			t.Errorf("%s: = %#08x, 期望 %#08x", c.name, got, c.want)
		}
	}
	// 起始小时落在 bit6..11,不能污染基址或结束字段
	v := encodeScheduleValue(1, 22, 23, true)
	if got := (v >> 6) & 0x3F; got != 22 {
		t.Errorf("起始小时位段解出 %d, 期望 22", got)
	}
	if got := (v >> 17) & 0x7F; got != 24 {
		t.Errorf("结束小时(+1)位段解出 %d, 期望 24", got)
	}
}
