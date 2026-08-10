package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
)

func main() {
	code := flag.String("code", "", "游戏登录 code")
	platform := flag.String("platform", "qq", "平台 qq/wx")
	osname := flag.String("os", "iOS", "系统 iOS/Android")
	flag.Parse()
	if *code == "" {
		fmt.Fprintln(os.Stderr, "用法: gwtest -code <真实code> [-platform wx] [-os iOS]")
		os.Exit(1)
	}

	cfg := gw.Config{
		ServerURL:       "wss://gate-obt.nqf.qq.com/prod/ws",
		ClientVersion:   "1.13.0.4_20260723",
		Platform:        *platform,
		OS:              *osname,
		HeartbeatMillis: 25000,
	}

	c := gw.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	fmt.Println("正在连接腾讯网关并登录...")
	start := time.Now()
	if err := c.Connect(ctx, *code); err != nil {
		fmt.Fprintf(os.Stderr, "连接/登录失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("登录成功! 耗时 %.2fs\n", time.Since(start).Seconds())
	fmt.Printf("  gid=%d\n  name=%s\n  level=%d\n  gold=%d\n", c.GID, c.UserName(), c.Level(), c.Gold())

	// 启动心跳保持连接
	c.StartHeartbeat(context.Background())
	fmt.Println("已启动心跳，连接保持中 (Ctrl+C 退出)...")

	// 保持连接，验证心跳稳定
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-sig:
			fmt.Println("\n退出")
			c.Close()
			return
		case <-ticker.C:
			fmt.Println("连接正常，10s 心跳窗口通过")
		}
	}
}
