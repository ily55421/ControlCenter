package main

import (
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
	w := wmi.Default
	as := appsettings.Default
	srv := ui.New(w, as, keyboard.Default, fan.Default, powermode.New(w, as, fan.Default, gpu.New(as, w)))
	addr, err := srv.Start()
	if err != nil { panic(err) }
	fmt.Println("UI:", addr)
	time.Sleep(300 * time.Millisecond)
	for _, p := range []string{"", "api/status", "api/mode", "api/fan/live", "api/kb/state", "api/battery", "api/cpu/info", "api/preference", "api/gpu/oc"} {
		r, err := http.Get(addr + p)
		if err != nil { fmt.Println("ERR", p, err); continue }
		b, _ := io.ReadAll(io.LimitReader(r.Body, 300))
		fmt.Printf("%-18s %d %s\n", p, r.StatusCode, string(b))
		r.Body.Close()
	}
}
