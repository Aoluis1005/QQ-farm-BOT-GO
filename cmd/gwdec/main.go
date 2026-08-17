package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/Aoluis1005/go-farm-bot/ace"
)

// 用法：gwdec <base64密文> [openID]
// decryptDataV2 4参数 = (inPtr, inLen, outPtr, outLenCap)，结果写到 outPtr，返回实际长度
func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: gwdec <b64> [openID]")
		return
	}
	data, _ := base64.StdEncoding.DecodeString(os.Args[1])
	openID := "owNAX6uJE35VdiRSQXSh9M7sY3bQ"
	if len(os.Args) >= 3 {
		openID = os.Args[2]
	}

	r := ace.New(openID, 3167, "0")
	if err := r.Init(context.Background()); err != nil {
		fmt.Println("init err:", err)
		return
	}
	defer r.Close()

	if err := r.BindUser(openID); err != nil {
		fmt.Println("bindUser err:", err)
		return
	}
	if _, err := r.EncryptedInitInfo(); err != nil {
		fmt.Println("encryptedInitInfo err:", err)
	}

	for _, method := range []string{"v1", "v2"} {
		out, err := r.DecryptBufferV2(data, method == "v2")
		if err != nil {
			fmt.Printf("%s err: %v\n", method, err)
			continue
		}
		fmt.Printf("%s len=%d\nhex: %X\nutf8: %q\n", method, len(out), out, string(out))
	}
}
