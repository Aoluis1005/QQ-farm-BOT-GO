package main

import (
	"context"
	"fmt"

	"github.com/Aoluis1005/go-farm-bot/ace"
)

// 打印 TSDK wasm 导出函数签名（诊断 decryptDataV2 的 4 参数）
func main() {
	r := ace.New("sig", 3167, "0")
	if err := r.Init(context.Background()); err != nil {
		fmt.Println("init err:", err)
		return
	}
	defer r.Close()
	for _, name := range []string{"ba", "ca", "da", "ea", "G", "H", "O", "P", "Q", "R"} {
		fmt.Println(r.DebugExportSignature(name))
	}
}
