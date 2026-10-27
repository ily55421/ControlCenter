// ccshot — 开发辅助:抓取 ccapp 真实窗口像素(视觉回归用,非 headless 模拟)。
//
// 用法:
//
//	ccshot                       // 只抓当前页
//	ccshot -pages system,kb,pref  // 依次导航后逐页抓
//	ccshot -out Temp/shots
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"controlcenter/internal/launcher"
	"controlcenter/internal/win32"
)

func main() {
	pages := flag.String("pages", "", "逗号分隔的页名;留空则只抓当前页")
	out := flag.String("out", "Temp/shots", "输出目录")
	title := flag.String("title", "Control Center", "窗口标题")
	settle := flag.Duration("settle", 700*time.Millisecond, "切页后等待动画收敛的时间")
	list := flag.Bool("list", false, "只列出可见顶层窗口(诊断同名窗口)")
	flag.Parse()

	if *list {
		for _, wi := range win32.TopWindows() {
			state := ""
			if wi.Icon {
				state = " [最小化]"
			}
			fmt.Printf("%-12d %-40q %dx%d @(%.0f,%.0f)%s\n",
				wi.HWND, wi.Title, wi.Rect.Dx(), wi.Rect.Dy(), float64(wi.Rect.Min.X), float64(wi.Rect.Min.Y), state)
		}
		return
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "建目录失败:", err)
		os.Exit(1)
	}
	want := []string{""}
	if *pages != "" {
		want = strings.Split(*pages, ",")
	}
	for _, p := range want {
		name := "current"
		if p != "" {
			name = launcher.NormalizePage(p)
			if !win32.NavNotify(name) {
				fmt.Fprintln(os.Stderr, "导航失败(ccapp 未运行?):", name)
				os.Exit(1)
			}
		}
		time.Sleep(*settle)
		if err := shot(*title, filepath.Join(*out, name+".png")); err != nil {
			fmt.Fprintln(os.Stderr, "截图失败:", err)
			os.Exit(1)
		}
	}
}

// shot 抓一帧窗口像素并写 PNG。
func shot(title, path string) error {
	img, err := win32.CaptureWindow(title)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	fmt.Printf("%s  %dx%d\n", path, img.Bounds().Dx(), img.Bounds().Dy())
	return nil
}
