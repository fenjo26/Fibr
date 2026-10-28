// Package bot implements a minimal Telegram bot: onboarding via /start with
// a Mini App button, /help and an admin /stats command. It talks to the Bot
// API over long polling, so no public webhook endpoint is required.
package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/fenjo26/Fibr/internal/api"
	"github.com/fenjo26/Fibr/internal/config"
	"github.com/fenjo26/Fibr/internal/store"
)

// StatsFunc supplies the /stats snapshot (wired to api.BuildOverview).
type StatsFunc func() (*api.Overview, error)

// Bot is the long-polling Telegram bot.
type Bot struct {
	token   string
	cfg     *config.Config
	store   *store.Store
	statsFn StatsFunc
	log     *slog.Logger
	http    *http.Client
	me      string
	offset  int64
}

// New creates the bot.
func New(cfg *config.Config, st *store.Store, statsFn StatsFunc, log *slog.Logger) *Bot {
	return &Bot{
		token:   cfg.TelegramBotToken,
		cfg:     cfg,
		store:   st,
		statsFn: statsFn,
		log:     log,
		http:    &http.Client{Timeout: 40 * time.Second},
	}
}

// Run polls updates until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	res, err := b.call(ctx, "getMe", nil)
	if err != nil {
		return fmt.Errorf("getMe: %w", err)
	}
	var me struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(res, &me); err != nil {
		return fmt.Errorf("getMe: %w", err)
	}
	b.me = me.Username
	b.log.Info("telegram bot running", "bot", "@"+b.me)

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		updates, err := b.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			b.log.Warn("getUpdates failed, backing off", "error", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			b.offset = u.UpdateID + 1
			b.handleUpdate(ctx, u)
		}
	}
}

/* ------------------------- telegram types ------------------------- */

type tgUser struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	FirstName    string `json:"first_name"`
	LanguageCode string `json:"language_code"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

type tgMessage struct {
	MessageID int64   `json:"message_id"`
	From      *tgUser `json:"from"`
	Chat      tgChat  `json:"chat"`
	Text      string  `json:"text"`
}

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

/* ------------------------- handling ------------------------- */

func (b *Bot) handleUpdate(ctx context.Context, u tgUpdate) {
	msg := u.Message
	if msg == nil || msg.From == nil {
		return
	}
	from := msg.From
	user, err := b.upsertUser(from)
	if err != nil {
		b.log.Error("bot: cannot upsert user", "telegram_id", from.ID, "error", err)
		return
	}
	if user.Disabled {
		return
	}
	lang := pickLang(from.LanguageCode)

	switch firstWord(msg.Text) {
	case "/start":
		b.sendMessage(ctx, msg.Chat.ID, t(lang, "welcome", b.me), b.miniAppKeyboard(lang))
	case "/help":
		b.sendMessage(ctx, msg.Chat.ID, t(lang, "help"), nil)
	case "/stats":
		if user.Role != "admin" {
			b.sendMessage(ctx, msg.Chat.ID, t(lang, "denied"), nil)
			return
		}
		ov, err := b.statsFn()
		if err != nil {
			b.sendMessage(ctx, msg.Chat.ID, t(lang, "statsError"), nil)
			return
		}
		b.sendMessage(ctx, msg.Chat.ID, formatStats(lang, ov), nil)
	case "/id":
		b.sendMessage(ctx, msg.Chat.ID, "Telegram ID: "+strconv.FormatInt(from.ID, 10), nil)
	default:
		b.sendMessage(ctx, msg.Chat.ID, t(lang, "unknown"), nil)
	}
}

func (b *Bot) upsertUser(from *tgUser) (*store.User, error) {
	if u, err := b.store.GetUserByTelegramID(from.ID); err == nil {
		if u.Role != "admin" && b.isAdminID(from.ID) {
			_ = b.store.SetUserRole(u.ID, "admin")
			u.Role = "admin"
		}
		return u, nil
	} else if err != store.ErrNotFound {
		return nil, err
	}
	role := "user"
	if b.isAdminID(from.ID) {
		role = "admin"
	}
	tid := from.ID
	name := from.Username
	if name == "" {
		name = from.FirstName
	}
	u := &store.User{TelegramID: &tid, Username: name, Role: role}
	if err := b.store.CreateUser(u); err != nil {
		return nil, err
	}
	b.log.Info("bot: user registered", "user_id", u.ID, "telegram_id", from.ID)
	return u, nil
}

func (b *Bot) isAdminID(id int64) bool {
	for _, x := range b.cfg.AdminTelegramIDs {
		if x == id {
			return true
		}
	}
	return false
}

// miniAppKeyboard returns the inline button opening the Mini App. Web App
// buttons require an https URL, so they are skipped in local dev.
func (b *Bot) miniAppKeyboard(lang string) any {
	if b.cfg.Domain == "" || b.cfg.Domain == "localhost" || b.cfg.Domain == "127.0.0.1" {
		return nil
	}
	return map[string]any{
		"inline_keyboard": [][]map[string]any{{
			{"text": t(lang, "openApp"), "web_app": map[string]string{"url": "https://" + b.cfg.Domain + "/miniapp/"}},
		}},
	}
}

/* ------------------------- bot api client ------------------------- */

func (b *Bot) getUpdates(ctx context.Context) ([]tgUpdate, error) {
	payload := map[string]any{"offset": b.offset, "timeout": 25, "allowed_updates": []string{"message"}}
	res, err := b.call(ctx, "getUpdates", payload)
	if err != nil {
		return nil, err
	}
	var updates []tgUpdate
	if err := json.Unmarshal(res, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func (b *Bot) sendMessage(ctx context.Context, chatID int64, text string, replyMarkup any) {
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	if _, err := b.call(ctx, "sendMessage", payload); err != nil {
		b.log.Warn("bot: sendMessage failed", "chat_id", chatID, "error", err)
	}
}

func (b *Bot) call(ctx context.Context, method string, payload any) (json.RawMessage, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	} else {
		body = []byte("{}")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+b.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("%s: %s", method, out.Description)
	}
	return out.Result, nil
}

/* ------------------------- texts & formatting ------------------------- */

func pickLang(code string) string {
	switch {
	case len(code) >= 2 && code[:2] == "ru", len(code) >= 2 && code[:2] == "uk", len(code) >= 2 && code[:2] == "be":
		return "ru"
	default:
		return "en"
	}
}

var texts = map[string]map[string]string{
	"ru": {
		"welcome":     "👋 Добро пожаловать в <b>Fibr</b>!\n\nКнопкой ниже откройте приложение: там можно создать устройство, забрать QR-код конфигурации и посмотреть свою статистику.",
		"help":        "Команды:\n/start — открыть приложение\n/help — справка\n/stats — статистика (только админ)\n/id — узнать свой Telegram ID",
		"denied":      "⛔ Команда доступна только администраторам.",
		"unknown":     "Не понял команду. /help — список команд.",
		"openApp":     "Открыть Fibr",
		"statsHeader": "📊 <b>Статистика Fibr</b>",
		"statsError":  "Не удалось собрать статистику, смотрите логи сервера.",
		"users":       "Пользователей",
		"devices":     "Устройств",
		"online":      "Онлайн",
		"traffic":     "Трафик",
		"tunnelRate":  "Туннель",
	},
	"en": {
		"welcome":     "👋 Welcome to <b>Fibr</b>!\n\nUse the button below to open the app: create a device, grab a QR config and check your traffic stats.",
		"help":        "Commands:\n/start — open the app\n/help — help\n/stats — stats (admin only)\n/id — show your Telegram ID",
		"denied":      "⛔ This command is admin-only.",
		"unknown":     "Unknown command. /help lists commands.",
		"openApp":     "Open Fibr",
		"statsHeader": "📊 <b>Fibr stats</b>",
		"statsError":  "Failed to collect stats, check server logs.",
		"users":       "Users",
		"devices":     "Devices",
		"online":      "Online",
		"traffic":     "Traffic",
		"tunnelRate":  "Tunnel",
	},
}

func t(lang, key string, args ...any) string {
	if s, ok := texts[lang][key]; ok {
		return fmt.Sprintf(s, args...)
	}
	return texts["en"][key]
}

func formatStats(lang string, ov *api.Overview) string {
	wgIn, wgOut := ov.Throughput.WgInBps, ov.Throughput.WgOutBps
	return fmt.Sprintf("%s\n%s: %d\n%s: %d\n%s: %d\n%s: ↓%s ↑%s\n%s: ↓%s ↑%s",
		t(lang, "statsHeader"),
		t(lang, "users"), ov.Totals.Users,
		t(lang, "devices"), ov.Totals.Devices,
		t(lang, "online"), ov.Totals.Online,
		t(lang, "traffic"), humanBytes(ov.Totals.TotalRx), humanBytes(ov.Totals.TotalTx),
		t(lang, "tunnelRate"), humanBits(wgIn), humanBits(wgOut),
	)
}

func firstWord(s string) string {
	for i, r := range s {
		if r == ' ' || r == '\n' || r == '@' {
			return s[:i]
		}
	}
	return s
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func humanBits(bps float64) string {
	const unit = 1000
	if bps < unit {
		return fmt.Sprintf("%.0f bit/s", bps)
	}
	div, exp := 1.0, 0
	for n := bps / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cbit/s", bps/div, "kMGTPE"[exp])
}
