package main

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/zzycxz/momapeer/internal/bot"
	"github.com/zzycxz/momapeer/internal/bot/feishu"
	"github.com/zzycxz/momapeer/internal/bot/qq"
	"github.com/zzycxz/momapeer/internal/bot/weixin"
	"github.com/zzycxz/momapeer/internal/config"
)

// startBotGateway 启动内嵌的 bot gateway。
// 调用方需确保 cfg.Bot.Enabled == true。
func (a *App) startBotGateway(cfg *config.Config) {
	if a.botGW.Load() != nil {
		return // already running
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// 构建 adapter
	adapters := make(map[bot.Platform]bot.Adapter)
	if cfg.Bot.QQ.Enabled {
		adapters[bot.PlatformQQ] = qq.New(cfg.Bot.QQ, logger)
	}
	if cfg.Bot.Feishu.Enabled {
		adapters[bot.PlatformFeishu] = feishu.New(cfg.Bot.Feishu, logger)
	}
	if cfg.Bot.Weixin.Enabled {
		adapters[bot.PlatformWeixin] = weixin.New(cfg.Bot.Weixin, logger)
	}

	if len(adapters) == 0 {
		logger.Warn("bot enabled but no channels enabled, skipping gateway start")
		return
	}

	// 构建 gateway 配置
	modelName := strings.TrimSpace(cfg.Bot.Model)
	if modelName == "" {
		modelName = cfg.DefaultModel
	}

	gwCfg := bot.GatewayConfig{
		Model:    modelName,
		MaxSteps: cfg.Bot.MaxSteps,
		Enabled: map[bot.Platform]bool{
			bot.PlatformQQ:     cfg.Bot.QQ.Enabled,
			bot.PlatformFeishu: cfg.Bot.Feishu.Enabled,
			bot.PlatformWeixin: cfg.Bot.Weixin.Enabled,
		},
		Allowlist: bot.AllowlistConfig{
			Enabled:  cfg.Bot.Allowlist.Enabled,
			AllowAll: cfg.Bot.Allowlist.AllowAll,
			Mode:     cfg.Bot.Allowlist.Mode,
			Users: map[bot.Platform][]string{
				bot.PlatformQQ:     cfg.Bot.Allowlist.QQUsers,
				bot.PlatformFeishu: cfg.Bot.Allowlist.FeishuUsers,
				bot.PlatformWeixin: cfg.Bot.Allowlist.WeixinUsers,
			},
			Groups: map[bot.Platform][]string{
				bot.PlatformQQ:     cfg.Bot.Allowlist.QQGroups,
				bot.PlatformFeishu: cfg.Bot.Allowlist.FeishuGroups,
				bot.PlatformWeixin: cfg.Bot.Allowlist.WeixinGroups,
			},
		},
		Channels: botChannelConfigsFromConnections(cfg.Bot.Connections),
		Debounce: time.Duration(cfg.Bot.DebounceMs) * time.Millisecond,
		AllowlistSaver: func(alCfg bot.AllowlistConfig) {
			if err := a.applyConfigOnly(func(c *config.Config) error {
				c.Bot.Allowlist.Enabled = alCfg.Enabled
				c.Bot.Allowlist.AllowAll = alCfg.AllowAll
				c.Bot.Allowlist.FeishuUsers = alCfg.Users[bot.PlatformFeishu]
				c.Bot.Allowlist.WeixinUsers = alCfg.Users[bot.PlatformWeixin]
				c.Bot.Allowlist.QQUsers = alCfg.Users[bot.PlatformQQ]
				return nil
			}); err != nil {
				logger.Warn("failed to persist allowlist", "err", err)
			}
		},
	}

	gw := bot.NewGateway(gwCfg, adapters, logger)
	if err := gw.Start(a.ctx); err != nil {
		logger.Error("bot gateway start failed", "err", err)
		return
	}

	a.botGW.Store(gw)

	logger.Info("bot gateway started", "model", modelName, "channels", len(adapters))
}

// stopBotGateway 停止内嵌的 bot gateway。
func (a *App) stopBotGateway() {
	gw := a.botGW.Swap(nil)
	if gw != nil {
		gw.Stop()
	}
}

// restartBotGateway 热重启 bot gateway（先停后启）。
func (a *App) restartBotGateway() {
	a.stopBotGateway()
	cfg, err := config.Load()
	if err != nil || !cfg.Bot.Enabled {
		return
	}
	a.startBotGateway(cfg)
}

// RecentChatView is the JSON-friendly projection of bot.RecentChat for the IM-
// target picker in the task form. Mirrors bot.RecentChat with json tags.
type RecentChatView struct {
	Platform string `json:"platform"`
	ChatType string `json:"chatType"`
	ChatID   string `json:"chatId"`
	UserName string `json:"userName"`
	LastSeen int64  `json:"lastSeen"`
}

// ListRecentBotChats returns recently-seen IM chats (newest first), so the task
// form can offer them as push destinations instead of forcing the user to
// hand-type a chatID. Returns [] when the bot isn't running (no chats seen yet).
func (a *App) ListRecentBotChats() []RecentChatView {
	gw := a.botGW.Load()
	if gw == nil {
		return []RecentChatView{}
	}
	src := gw.RecentChats()
	out := make([]RecentChatView, 0, len(src))
	for _, c := range src {
		out = append(out, RecentChatView{
			Platform: string(c.Platform),
			ChatType: string(c.ChatType),
			ChatID:   c.ChatID,
			UserName: c.UserName,
			LastSeen: c.LastSeen,
		})
	}
	return out
}

// botChannelConfigsFromConnections 从 connections 配置中提取每个平台的覆盖参数。
func botChannelConfigsFromConnections(connections []config.BotConnectionConfig) map[bot.Platform]bot.ChannelConfig {
	if len(connections) == 0 {
		return nil
	}
	out := make(map[bot.Platform]bot.ChannelConfig)
	for _, conn := range connections {
		if !conn.Enabled {
			continue
		}
		plat := bot.Platform(strings.TrimSpace(conn.Provider))
		switch plat {
		case bot.PlatformQQ, bot.PlatformFeishu, bot.PlatformWeixin:
		default:
			continue
		}
		channel := out[plat]
		if v := strings.TrimSpace(conn.Model); v != "" {
			channel.Model = v
		}
		if v := strings.TrimSpace(conn.WorkspaceRoot); v != "" {
			channel.WorkspaceRoot = v
		}
		if channel.Model != "" || channel.WorkspaceRoot != "" {
			out[plat] = channel
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
