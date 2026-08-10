package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
)

const plantService = "gamepb.plantpb.PlantService"

// 普通化肥 ID（对齐 Node NORMAL_FERTILIZER_ID）
const normalFertilizerID = 1011

var opLogMu sync.Mutex

// execFarmOp 执行一个农场操作
func execFarmOp(c *gw.Client, method string, body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	_, err := c.Request(ctx, plantService, method, body, 12*time.Second)
	return err
}

func parseIDs(s string) []int64 {
	var out []int64
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '，' }) {
		if v, err := strconv.ParseInt(part, 10, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// appendOpLog 写入操作日志文件
func appendOpLog(accountID, action, detail string) {
	opLogMu.Lock()
	defer opLogMu.Unlock()
	dir := filepath.Join(dataDir, "logs")
	os.MkdirAll(dir, 0755)
	f, err := os.OpenFile(filepath.Join(dir, accountID+".log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s %s\n", time.Now().Format("01-02 15:04:05"), action, detail)
}

// readOpLogs 读取最近 N 条操作日志
func readOpLogs(accountID string, n int) []string {
	opLogMu.Lock()
	defer opLogMu.Unlock()
	f, err := os.Open(filepath.Join(dataDir, "logs", accountID+".log"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
