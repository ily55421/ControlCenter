package gpu

import "testing"

// TestEncodeDecodeInt16 验证 page5 非对称 int16 编码的往返一致性:
// 写侧 n<0 先 -1 再截 16 位;读侧高位为 1 时 raw-65535。
func TestEncodeDecodeInt16(t *testing.T) {
	// 注:-32768 经非对称编码(n-1)越界,协议有效范围为 -1600..32767
	cases := []int{0, 1, 100, 400, 1600, 32767, -1, -5, -200, -1600}
	for _, n := range cases {
		enc := EncodeInt16(n)
		if len(enc) != 2 {
			t.Fatalf("EncodeInt16(%d) 长度 = %d, 期望 2", n, len(enc))
		}
		got := DecodeInt16(enc)
		if got != n {
			t.Errorf("往返失败:EncodeInt16(%d)=%v → DecodeInt16=%d", n, enc, got)
		}
	}
}

// TestEncodeInt16KnownBytes 校验具体字节布局(小端)。
func TestEncodeInt16KnownBytes(t *testing.T) {
	// -1 → n-1=-2 → int16 0xFFFE → 小端 [0xFE,0xFF]
	enc := EncodeInt16(-1)
	if enc[0] != 0xFE || enc[1] != 0xFF {
		t.Errorf("EncodeInt16(-1) = %#v, 期望 [0xFE 0xFF]", enc)
	}
	// 解码侧:0xFFFE 高位为 1 → 65534-65535 = -1
	if got := DecodeInt16([]byte{0xFE, 0xFF}); got != -1 {
		t.Errorf("DecodeInt16([0xFE 0xFF]) = %d, 期望 -1", got)
	}
	// 正数不调整
	enc = EncodeInt16(100)
	if enc[0] != 100 || enc[1] != 0 {
		t.Errorf("EncodeInt16(100) = %#v, 期望 [100 0]", enc)
	}
}

// TestControlGPUPath 路径推测:空目录回退默认安装路径,非空目录拼接。
func TestControlGPUPath(t *testing.T) {
	if p := ControlGPUPath(""); p != `C:\Program Files (x86)\ControlCenter\DCHU\ControlGPU.exe` {
		t.Errorf("ControlGPUPath(\"\") = %q", p)
	}
	if p := ControlGPUPath(`D:\cc`); p != `D:\cc\ControlGPU.exe` {
		t.Errorf("ControlGPUPath(D:\\cc) = %q", p)
	}
}
