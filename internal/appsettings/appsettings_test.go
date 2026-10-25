package appsettings

import (
	"sync"
	"testing"
	"time"
)

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

// fakeBridge 是设置区替身:读写之间插入延迟,放大"读旧值→改→写回"的竞态窗口。
// 若复合操作没有整体持锁,下面的用例就会丢更新。
type fakeBridge struct {
	mu    sync.Mutex
	pages [16][256]byte
	delay time.Duration
}

func (f *fakeBridge) ReadAppSettings(page, offset, length int) ([]byte, error) {
	f.mu.Lock()
	out := append([]byte{}, f.pages[page][offset:offset+length]...)
	f.mu.Unlock()
	time.Sleep(f.delay)
	return out, nil
}

func (f *fakeBridge) WriteAppSettings(page, offset, length int, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy(f.pages[page][offset:], data)
	time.Sleep(f.delay)
	return nil
}

// TestUpdateU8AtomicAcrossGoroutines 并发置同一字节的不同位,全部必须保留。
func TestUpdateU8AtomicAcrossGoroutines(t *testing.T) {
	f := &fakeBridge{delay: time.Millisecond}
	s := &Store{b: f}
	var wg sync.WaitGroup
	for bit := 0; bit < 8; bit++ {
		wg.Add(1)
		go func(b int) {
			defer wg.Done()
			if err := s.UpdateU8(0x100, func(cur byte) byte { return cur | (1 << b) }); err != nil {
				t.Errorf("UpdateU8(bit %d): %v", b, err)
			}
		}(bit)
	}
	wg.Wait()
	if got := f.pages[1][0]; got != 0xFF {
		t.Fatalf("8 路并发置位后 = %#x, 期望 0xff(说明读改写未在同一把锁内,存在丢失更新)", got)
	}
}

// TestWithPagePreservesUntouchedBytes 整页写回必须先读当前页,不得覆盖无关字节。
func TestWithPagePreservesUntouchedBytes(t *testing.T) {
	f := &fakeBridge{}
	for i := range f.pages[4] {
		f.pages[4][i] = 0x5A
	}
	s := &Store{b: f}
	if err := s.WithPage(4, func(pg []byte) { pg[6] = 2; pg[20] = 77 }); err != nil {
		t.Fatal(err)
	}
	if f.pages[4][6] != 2 || f.pages[4][20] != 77 {
		t.Fatalf("目标字节未写入: [6]=%d [20]=%d", f.pages[4][6], f.pages[4][20])
	}
	if f.pages[4][0] != 0x5A || f.pages[4][128] != 0x5A || f.pages[4][255] != 0x5A {
		t.Fatalf("未修改的字节被整页写覆盖")
	}
}

// TestPageWritesSerialize 同页的整页改写与单字节改写并发时不得互相覆盖。
func TestPageWritesSerialize(t *testing.T) {
	f := &fakeBridge{delay: time.Millisecond}
	s := &Store{b: f}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			if err := s.WithPage(4, func(pg []byte) { pg[16+i] = byte(0xA0 + i) }); err != nil {
				t.Errorf("WithPage(%d): %v", i, err)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			if err := s.WriteU8(0x400+100+i, byte(0xB0+i)); err != nil {
				t.Errorf("WriteU8(%d): %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < 4; i++ {
		if got := f.pages[4][16+i]; got != byte(0xA0+i) {
			t.Errorf("page4[%d] = %#x, 期望 %#x(整页改写被并发覆盖)", 16+i, got, 0xA0+i)
		}
		if got := f.pages[4][100+i]; got != byte(0xB0+i) {
			t.Errorf("page4[%d] = %#x, 期望 %#x(单字节写被整页写覆盖)", 100+i, got, 0xB0+i)
		}
	}
}
