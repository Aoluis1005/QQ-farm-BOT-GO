package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

func main() {
	code := flag.String("code", "", "游戏登录 code")
	platform := flag.String("platform", "wx", "平台 qq/wx")
	osname := flag.String("os", "iOS", "系统")
	flag.Parse()
	if *code == "" {
		fmt.Fprintln(os.Stderr, "用法: gwdata -code <code> -platform wx")
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
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	fmt.Println("连接+登录...")
	if err := c.Connect(ctx, *code); err != nil {
		fmt.Fprintln(os.Stderr, "登录失败:", err)
		os.Exit(1)
	}
	fmt.Printf("登录成功: %s (gid=%d, Lv%d, gold=%d)\n", c.UserName(), c.GID, c.Level(), c.Gold())
	// 打包资产
	if err := c.FetchBagAssets(ctx); err != nil {
		fmt.Println("背包拉取失败:", err)
	} else {
		fmt.Printf("点券(coupon)=%d  金豆(goldBean)=%d\n", c.Coupon(), c.GoldBean())
	}
	c.StartHeartbeat(context.Background())

	// 拉取全部地块
	fmt.Println("\n===== 拉取农场 AllLands =====")
	rep, err := c.Request(ctx, "gamepb.plantpb.PlantService", "AllLands",
		proto.EncodeAllLandsRequest(0), 15*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "AllLands 失败:", err)
		os.Exit(1)
	}
	lands := proto.DecodeAllLandsReply(rep.Body)
	fmt.Printf("共 %d 块地:\n", len(lands.Lands))
	for _, l := range lands.Lands {
		status := "🔒"
		if l.Unlocked {
			status = "🌱"
		}
		crop := "-"
		if l.Plant != nil {
			crop = fmt.Sprintf("%s(id=%d) 果%d 剩%d 长%ds 缺%d",
				l.Plant.Name, l.Plant.ID, l.Plant.FruitNum, l.Plant.LeftFruitNum,
				l.Plant.GrowSec, l.Plant.DryNum)
			if l.Plant.Stealable {
				crop += " 可偷"
			}
		}
		fmt.Printf("  地%d %s Lv%d/%d 作物:%s\n", l.ID, status, l.Level, l.MaxLevel, crop)
	}

	c.Close()
	fmt.Println("\n完成")
}
