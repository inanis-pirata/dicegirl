// Command qqbot is the QQ official-bot adapter for dicegirl. It only
// translates platform events into scoped chatcore command lines (see the
// bot.Manager) and posts the rendered replies back; all TRPG behavior is
// shared with the CLI through chatcore + internal/app.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"dicegirl/bot"
	"dicegirl/internal/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := configFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "qqbot 配置错误：", err)
		os.Exit(2)
	}

	logger, logCloser, err := newLogger(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "qqbot 日志初始化失败：", err)
		os.Exit(1)
	}
	if logCloser != nil {
		defer logCloser.Close()
	}
	manager := bot.NewManager(cfg.DataRoot)
	defer manager.CloseAll()

	contacts, err := loadContactStore(cfg.DataRoot)
	if err != nil {
		logger.Print("联系人存储加载失败：", err)
		os.Exit(1)
	}

	api := newAPIClient(cfg, logger)
	bh := newBotHandler(cfg, manager, api, contacts, logger)
	gw := newGateway(api, bh.handle, logger, cfg.Debug)

	logger.Printf("启动：版本=%s AppID=%s 数据根=%s API=%s 非指令提示=%t 调试=%t 日志文件=%q",
		version.Version, cfg.AppID, cfg.DataRoot, cfg.APIBase, cfg.ReplyUnknown, cfg.Debug, cfg.LogFile)
	if err := gw.run(ctx); err != nil {
		logger.Print("网关终止：", err)
		os.Exit(1)
	}
	logger.Print("已退出")
}
