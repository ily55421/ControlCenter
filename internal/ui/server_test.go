package ui

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLimitBodyRejectsOversized 请求体超过上限时,handler 读 body 必须失败,
// 不能把超大输入喂给 JSON 解码。
func TestLimitBodyRejectsOversized(t *testing.T) {
	var readErr error
	h := limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	oversize := strings.Repeat("A", maxBodyBytes+1024)
	req := httptest.NewRequest(http.MethodPost, "/api/any", strings.NewReader(oversize))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if readErr == nil {
		t.Fatalf("超限请求体被完整读出,MaxBytesReader 未生效")
	}
	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Fatalf("错误类型 = %T(%v), 期望 *http.MaxBytesError", readErr, readErr)
	}
}

// TestLimitBodyAllowsNormalSize 正常大小的配置 JSON 不受影响。
func TestLimitBodyAllowsNormalSize(t *testing.T) {
	var body string
	h := limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取正常大小请求体失败: %v", err)
		}
		body = string(b)
	}))
	payload := `{"tables":[{"t1":50,"t2":70,"t3":85,"d1":30,"d2":60,"d3":90}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/fan/table", strings.NewReader(payload))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if body != payload {
		t.Fatalf("请求体被截断: %q", body)
	}
}

// TestNewHTTPServerTimeouts 服务器必须带超时,慢连接不能无限占用。
func TestNewHTTPServerTimeouts(t *testing.T) {
	srv := newHTTPServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout <= 0 {
		t.Errorf("ReadHeaderTimeout = %v, 需要 > 0", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Errorf("超时未全部配置: read=%v write=%v idle=%v",
			srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
}

// TestHandlerServesIndex 首页与整张路由表可在无 ACPI 设备/未初始化依赖时构造并响应,
// 保证 /api/* 的注册不会在启动阶段触碰硬件。
func TestHandlerServesIndex(t *testing.T) {
	h := (&Server{}).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("首页状态码 = %d, 期望 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("首页 Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `fetch("/api/"+`) {
		t.Errorf("首页未包含面板 SPA 内容")
	}
}
