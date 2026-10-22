package main

// Bot configuration is environment-only: credentials must never be committed
// or embedded in the binary.
//
//	DICEGIRL_QQ_APPID        机器人 AppID
//	DICEGIRL_QQ_SECRET       机器人 AppSecret
//	DICEGIRL_BOT_DATA_ROOT   多租户数据基座（默认 ./botdata，其下 groups/、c2c/）
//	DICEGIRL_QQ_API_BASE     开放平台主机（默认 https://api.bot.qq.com，仅排障时覆盖）
//	DICEGIRL_BOT_REPLY_UNKNOWN  非指令的@消息是否回复提示（默认 false：静默忽略）
//	DICEGIRL_BOT_LOG_FILE    同时写入日志文件（默认只写 stdout / journald，自动轮转）

import (
	"os"
	"strconv"

	"dicegirl/internal/apperr"
)

const (
	envAppID        = "DICEGIRL_QQ_APPID"
	envSecret       = "DICEGIRL_QQ_SECRET"
	envDataRoot     = "DICEGIRL_BOT_DATA_ROOT"
	envAPIBase      = "DICEGIRL_QQ_API_BASE"
	envReplyUnknown = "DICEGIRL_BOT_REPLY_UNKNOWN"
	envLogFile      = "DICEGIRL_BOT_LOG_FILE"

	defaultBotDataRoot = "botdata"
	defaultAPIBase     = "https://api.bot.qq.com"
)

// botConfig is the resolved adapter configuration.
type botConfig struct {
	AppID        string
	Secret       string
	DataRoot     string
	APIBase      string
	ReplyUnknown bool
	// LogFile, when set, receives a copy of every log line (rotated).
	LogFile string
}

func configFromEnv() (*botConfig, error) {
	cfg := &botConfig{
		AppID:    os.Getenv(envAppID),
		Secret:   os.Getenv(envSecret),
		DataRoot: os.Getenv(envDataRoot),
		APIBase:  os.Getenv(envAPIBase),
	}
	if cfg.AppID == "" || cfg.Secret == "" {
		return nil, apperr.New("qqbot.configFromEnv", apperr.InvalidArgument,
			"缺少凭据：请设置环境变量 "+envAppID+" 与 "+envSecret+"（切勿写入仓库）")
	}
	if cfg.DataRoot == "" {
		cfg.DataRoot = defaultBotDataRoot
	}
	if cfg.APIBase == "" {
		cfg.APIBase = defaultAPIBase
	}
	cfg.LogFile = os.Getenv(envLogFile)
	if v := os.Getenv(envReplyUnknown); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, apperr.Wrap("qqbot.configFromEnv", apperr.InvalidArgument,
				envReplyUnknown+" 必须是 true/false 或 1/0", err)
		}
		cfg.ReplyUnknown = b
	}
	return cfg, nil
}
