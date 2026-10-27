// fontchars — 提取 GUI 运行期可能渲染的全部字符,供 fonttools 子集化内嵌字体。
//
// 只收集字符串/字符字面量:动态文本(硬件型号、电源计划名)靠系统字体回退兜底。
// 用法: go run ./cmd/fontchars ./internal ./cmd > Temp/fontchars.txt
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"./internal", "./cmd"}
	}
	set := map[rune]bool{}
	for _, root := range roots {
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.Contains(p, "_test.go") {
				return nil
			}
			collect(p, set)
			return nil
		})
	}
	// 保证排版基元与常用标点始终可用
	for _, r := range " .,:;!?()[]{}<>-—…·•|/\\%+=*#@&~`'\"_0123456789" +
		"≡▾▴▲▼◆◇○●◐☀☂⚡✓✗°%←→↑↓↔«»±≈≤≥℃℉" {
		set[r] = true
	}
	// 中日韩标点与全/半角形式整段纳入:硬件型号与电源计划名常带这些字符
	for r := rune(0x3000); r <= 0x303F; r++ {
		set[r] = true
	}
	for r := rune(0xFF00); r <= 0xFF5E; r++ {
		set[r] = true
	}
	var out []rune
	for r := range set {
		if r < 0x20 {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	fmt.Print(string(out))
}

// collect 解析单个文件,把字面量里的 rune 并入 set。
func collect(path string, set map[rune]bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BasicLit:
			if v.Kind != token.STRING && v.Kind != token.CHAR {
				return true
			}
			s, err := strconv.Unquote(v.Value)
			if err != nil {
				return true
			}
			for _, r := range s {
				set[r] = true
			}
		}
		return true
	})
}
