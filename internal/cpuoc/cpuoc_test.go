package cpuoc

import (
	"encoding/binary"
	"math"
	"testing"
)

// pageWith 按"偏移→长度"写入小端值,构造一张 page6 快照。
func pageWith(t *testing.T, vals map[int]any) []byte {
	t.Helper()
	pg := make([]byte, 256)
	for off, v := range vals {
		switch x := v.(type) {
		case byte:
			pg[off] = x
		case int32:
			binary.LittleEndian.PutUint32(pg[off:], uint32(x))
		case int16:
			binary.LittleEndian.PutUint16(pg[off:], uint16(x))
		case uint16:
			binary.LittleEndian.PutUint16(pg[off:], x)
		case float32:
			binary.LittleEndian.PutUint32(pg[off:], math.Float32bits(x))
		default:
			t.Fatalf("offset %d: 不支持的测试值类型 %T", off, v)
		}
	}
	return pg
}

// TestReadInfoFromOffsets 钉住 page6 只读区偏移(改成整页快照解码时,
// 任何一个绝对→页内偏移的换算错误都会静默改变面板显示)。
func TestReadInfoFromOffsets(t *testing.T) {
	pg := pageWith(t, map[int]any{
		9:   int32(3200), // CPUMHz
		13:  float32(1.25),
		21:  byte(47),
		22:  byte(68),
		23:  int32(4800),
		224: byte(57),
		225: byte(8),
		226: uint16(4800),
		228: uint16(800),
		230: uint16(5600),
		232: uint16(100),
		251: byte(0),  // XTUBusy = (251==0)
		32:  byte(1),  // IsSaved
	})
	got := ReadInfoFrom(pg)
	want := Info{
		CPUMHz: 3200, CPUVolt: 1.25, CPUUtil: 47, CPUTemp: 68, MemMHz: 4800,
		RatioMax: 57, RatioMin: 8, TurboMaxMHz: 4800, TurboMinMHz: 800,
		MemMaxMHz: 5600, RefClock: 100, XTUBusy: true, IsSaved: true,
	}
	if got != want {
		t.Errorf("ReadInfoFrom = %+v\n期望 %+v", got, want)
	}
}

// TestReadParamsFromOffsets 钉住当前参数区偏移与有符号语义。
func TestReadParamsFromOffsets(t *testing.T) {
	pg := make([]byte, 256)
	coreOff, gfxOff := int32(-80), int32(-50)
	binary.LittleEndian.PutUint32(pg[33:], 125)  // PL1
	binary.LittleEndian.PutUint32(pg[37:], 150)  // PL2
	binary.LittleEndian.PutUint32(pg[41:], math.Float32bits(2.8))
	pg[74] = 1 // VRAuto
	binary.LittleEndian.PutUint32(pg[49:], 140)  // VRLimit
	binary.LittleEndian.PutUint32(pg[61:], uint32(coreOff)) // CoreOffset 负值
	binary.LittleEndian.PutUint32(pg[65:], math.Float32bits(1.35))
	binary.LittleEndian.PutUint32(pg[69:], uint32(gfxOff)) // GraphicsOff
	for i := 0; i < 8; i++ {
		pg[53+i] = byte(40 + i)
	}
	pg[75], pg[76] = 48, 49
	for i := 0; i < 16; i++ {
		pg[77+i] = byte(20 + i)
	}

	p := ReadParamsFrom(pg)
	if p.PL1Watt != 125 || p.PL2Watt != 150 || p.VRLimit != 140 || !p.VRAuto {
		t.Errorf("功耗/VR 字段解码错误: %+v", p)
	}
	if p.CoreOffset != -80 {
		t.Errorf("CoreOffset = %d, 期望 -80(必须按有符号 int32 解,否则电压偏移会变成巨大正数)", p.CoreOffset)
	}
	if p.GraphicsOff != -50 {
		t.Errorf("GraphicsOff = %d, 期望 -50", p.GraphicsOff)
	}
	if math.Abs(float64(p.PLTimeSec-2.8)) > 1e-6 || math.Abs(float64(p.OverrideVolt-1.35)) > 1e-6 {
		t.Errorf("float 字段解码错误: PLTime=%v Override=%v", p.PLTimeSec, p.OverrideVolt)
	}
	for i := 0; i < 8; i++ {
		if p.PCores[i] != 40+i {
			t.Errorf("PCores[%d] = %d, 期望 %d", i, p.PCores[i], 40+i)
		}
	}
	if p.PCores[8] != 48 || p.PCores[9] != 49 {
		t.Errorf("PCores[8..9] = %d,%d,期望 48,49(这两个倍频单独放在 (6,75)(6,76))", p.PCores[8], p.PCores[9])
	}
	for i := 0; i < 16; i++ {
		if p.ECores[i] != 20+i {
			t.Errorf("ECores[%d] = %d, 期望 %d", i, p.ECores[i], 20+i)
			break
		}
	}
}

// TestReadDefaultsFromOffsets 钉住默认值区偏移。
//
// 注意:这里刻意把"整型组"和"浮点组"分两次断言 —— 当前实现里 PL2 读 (6,112) 4 字节
// (112..115) 与 PLTime 读 (6,114) float(114..117)**字节区间重叠**,两者同时非零时
// 后写的浮点会污染 PL2 高位。原版的默认值实际来自 WMI18 包(带 /1e6、/1e3 缩放),
// 我们既没实现 WMI18 也没核对过这段布局,已作为缺陷单独上报,不在本次性能改动里顺手猜改。
func TestReadDefaultsFromOffsets(t *testing.T) {
	ints := make([]byte, 256)
	dCoreOff, dGfxOff := int32(-80), int32(-50)
	binary.LittleEndian.PutUint16(ints[110:], 125)
	binary.LittleEndian.PutUint32(ints[112:], 150)
	ints[118] = 1
	binary.LittleEndian.PutUint32(ints[119:], 140)
	binary.LittleEndian.PutUint32(ints[123:], uint32(dCoreOff))
	binary.LittleEndian.PutUint32(ints[131:], uint32(dGfxOff))
	for i := 0; i < 10; i++ {
		ints[135+i] = byte(30 + i)
	}
	for i := 0; i < 16; i++ {
		ints[145+i] = byte(10 + i)
	}
	d := ReadDefaultsFrom(ints)
	if d.PL1 != 125 || d.PL2 != 150 || d.VR != 140 || !d.VRAuto {
		t.Errorf("默认功耗/VR 解码错误: %+v", d)
	}
	if d.CoreOff != -80 || d.GfxOff != -50 {
		t.Errorf("默认偏移符号错误: CoreOff=%d GfxOff=%d", d.CoreOff, d.GfxOff)
	}
	if d.PCores[9] != 39 || d.ECores[15] != 25 {
		t.Errorf("默认倍频区末端解码错误: P[9]=%d E[15]=%d", d.PCores[9], d.ECores[15])
	}

	floats := make([]byte, 256)
	binary.LittleEndian.PutUint32(floats[114:], math.Float32bits(2.8))
	binary.LittleEndian.PutUint32(floats[127:], math.Float32bits(1.35))
	df := ReadDefaultsFrom(floats)
	if math.Abs(float64(df.PLTime-2.8)) > 1e-6 || math.Abs(float64(df.Override-1.35)) > 1e-6 {
		t.Errorf("默认 float 解码错误: PLTime=%v Override=%v", df.PLTime, df.Override)
	}
}

// TestReadFromShortPageSafe 页快照长度不足时不得 panic(驱动读失败会给出短缓冲)。
func TestReadFromShortPageSafe(t *testing.T) {
	pg := make([]byte, 10)
	if got := ReadInfoFrom(pg); got.CPUMHz != 0 {
		t.Errorf("短页应解出零值,得到 %+v", got)
	}
	_ = ReadParamsFrom(pg)
	_ = ReadDefaultsFrom(pg)
}
