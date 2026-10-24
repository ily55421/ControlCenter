package acpibridge

import (
	"encoding/binary"
	"testing"
)

// 验证 WMI 求值输入结构布局与逆向还原的原生 DLL 完全一致。
func TestBuildWMIInput(t *testing.T) {
	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}
	in := buildWMIInput(121, payload)

	if len(in) != 0x420 {
		t.Fatalf("输入长度 = %#x, want 0x420", len(in))
	}
	wantGUID := []byte{0xe4, 0x24, 0xf2, 0x93, 0xdc, 0xfb, 0xbf, 0x4b,
		0xad, 0xd6, 0xdb, 0x71, 0xbd, 0xc0, 0xaf, 0xad}
	if string(in[0:16]) != string(wantGUID) {
		t.Fatalf("GUID 字节不匹配: % x", in[0:16])
	}
	if got := binary.LittleEndian.Uint32(in[0x10:0x14]); got != 0 {
		t.Fatalf("0x10 处应为 0, got %#x", got)
	}
	if string(in[0x14:0x18]) != "_DSM" {
		t.Fatalf("方法名 = %q, want _DSM", in[0x14:0x18])
	}
	if got := binary.LittleEndian.Uint32(in[0x18:0x1c]); got != 121 {
		t.Fatalf("命令号 = %d, want 121", got)
	}
	if got := binary.LittleEndian.Uint16(in[0x1c:0x1e]); got != 0x0104 {
		t.Fatalf("0x1C 处 = %#x, want 0x104", got)
	}
	if got := binary.LittleEndian.Uint32(in[0x1e:0x22]); got != 0x01000002 {
		t.Fatalf("0x1E 处 = %#x, want 0x01000002", got)
	}
	if in[0x22] != 0 || in[0x23] != 1 {
		t.Fatalf("载荷起始字节不匹配: % x", in[0x22:0x24])
	}
	if in[0x22+0xFF] != 0xFF {
		t.Fatalf("载荷第 255 字节不匹配")
	}
}

// 验证输出条目解析(整数 + 缓冲 + 最小步长)。
func TestParseWMIOutput(t *testing.T) {
	out := make([]byte, 0x40C)
	binary.LittleEndian.PutUint32(out[8:12], 3) // count
	// 条目 1: type=0(整数), size=8, data @+4
	off := 0xC
	binary.LittleEndian.PutUint16(out[off:], 0)
	binary.LittleEndian.PutUint16(out[off+2:], 4)
	binary.LittleEndian.PutUint32(out[off+4:], 0xABCD)
	// 条目 2: type=2(缓冲), dataSize=6
	off += 8
	binary.LittleEndian.PutUint16(out[off:], 2)
	binary.LittleEndian.PutUint16(out[off+2:], 6)
	copy(out[off+4:], "hello!")
	// 条目 3: type=0, dataSize=4
	off += 10
	binary.LittleEndian.PutUint16(out[off:], 0)
	binary.LittleEndian.PutUint16(out[off+2:], 4)
	binary.LittleEndian.PutUint32(out[off+4:], 7)

	entries, err := parseWMIOutput(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("条目数 = %d, want 3", len(entries))
	}
	if entries[0].Type != 0 || binary.LittleEndian.Uint32(entries[0].Data) != 0xABCD {
		t.Fatalf("整数条目解析错误: %+v", entries[0])
	}
	if entries[1].Type != 2 || string(entries[1].Data) != "hello!" {
		t.Fatalf("缓冲条目解析错误: %+v", entries[1])
	}
	if entries[2].Type != 0 || binary.LittleEndian.Uint32(entries[2].Data) != 7 {
		t.Fatalf("零尺寸条目解析错误: %+v", entries[2])
	}
}
