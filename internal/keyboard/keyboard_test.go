package keyboard

import "testing"

// TestPerkeyAddr 单键地址公式:640 + row*64 + col*3 + col/5(与原版 Perkey 矩阵一致)。
func TestPerkeyAddr(t *testing.T) {
	cases := []struct{ row, col, want int }{
		{0, 0, 640},
		{0, 1, 643},
		{0, 4, 652},
		{0, 5, 656}, // 每 5 列多 1 字节间隙
		{0, 10, 672},
		{1, 0, 704},
		{2, 3, 777},
	}
	for _, c := range cases {
		if got := PerkeyAddr(c.row, c.col); got != c.want {
			t.Errorf("PerkeyAddr(%d,%d) = %d, 期望 %d", c.row, c.col, got, c.want)
		}
	}
}
