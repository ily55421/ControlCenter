package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"

	"controlcenter/internal/appsettings"
	"controlcenter/internal/fan"
	"controlcenter/internal/gpu"
	"controlcenter/internal/keyboard"
	"controlcenter/internal/powermode"
	"controlcenter/internal/ui"
	"controlcenter/internal/wmi"
)

func main() {
	hold := flag.Bool("hold", false, "探测后保持服务常驻(供浏览器视觉验证)")
	port := flag.Int("port", 0, "固定端口(0=随机)")
	flag.Parse()
	w := wmi.Default
	as := appsettings.Default
	srv := ui.New(w, as, keyboard.Default, fan.Default, powermode.New(w, as, fan.Default, gpu.New(as, w)))
	var addr string
	var err error
	if *port > 0 {
		if _, err = srv.ServeOn(*port); err != nil {
			panic(err)
		}
		addr = fmt.Sprintf("http://127.0.0.1:%d/", *port)
	} else if addr, err = srv.Start(); err != nil {
		panic(err)
	}
	if err != nil { panic(err) }
	fmt.Println("UI:", addr)
	time.Sleep(300 * time.Millisecond)
	for _, p := range []string{"", "api/status", "api/mode", "api/fan/live", "api/kb/state", "api/battery", "api/cpu/info", "api/preference", "api/gpu/oc", "api/energy/sample", "api/flexikey/state"} {
		r, err := http.Get(addr + p)
		if err != nil { fmt.Println("ERR", p, err); continue }
		b, _ := io.ReadAll(io.LimitReader(r.Body, 300))
		fmt.Printf("%-18s %d %s\n", p, r.StatusCode, string(b))
		r.Body.Close()
	}
	if *hold {
		fmt.Println("hold 模式:服务常驻,Ctrl+C 退出")
		select {}
	}
}
