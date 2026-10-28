// Package bot implements a minimal Telegram bot: onboarding via /start with
// a Mini App button, /help and an admin /stats command. It talks to the Bot
// API over long polling, so no public webhook endpoint is required.
package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// FetchMe returns the bot's @username with a single API call; main.go uses
// it to publish the name to /api/public/info for the web cabinet widget.
func (b *Bot) FetchMe() (string, error) {
	res, err := b.call(context.Background(), "getMe", nil)
	if err != nil {
		return "", fmt.Errorf("getMe: %w", err)
	}
	var me struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(res, &me); err != nil {
		return "", fmt.Errorf("getMe: %w", err)
	}
	return me.Username, nil
}

// Run polls updates until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	name, err := b.FetchMe()
	if err != nil {
		return err
	}
	b.me = name
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
	// A t.me/<bot>?start=ref_CODE / ?start=inv_CODE deep link arrives as
	// "/start ref_CODE" / "/start inv_CODE"; the payload decides whether this
	// is a referral or an invite arrival.
	refCode, inviteCode := "", ""
	if cmd := firstWord(msg.Text); cmd == "/start" {
		refCode, inviteCode = startPayload(msg.Text)
	}
	// Invite-only mode: the bot must NOT register strangers. Creating the
	// account here let anyone skip the invite: send /start, then open the
	// Mini App as an "existing" user (the gate only checks brand-new users).
	// Unknown users get the app button carrying their invite/ref code and
	// the Mini App sign-in enforces the gate and claims the one-time code.
	if b.cfg.RequireInvite {
		if _, err := b.store.GetUserByTelegramID(from.ID); err == store.ErrNotFound {
			lang := pickLang(from.LanguageCode)
			if inviteCode != "" || refCode != "" {
				b.sendMessage(ctx, msg.Chat.ID, t(lang, "welcomeInvite"), b.miniAppKeyboard(lang, refCode, inviteCode))
			} else {
				b.sendMessage(ctx, msg.Chat.ID, t(lang, "inviteOnly"), nil)
			}
			return
		} else if err != nil {
			b.log.Error("bot: user lookup failed", "telegram_id", from.ID, "error", err)
			return
		}
	}
	user, err := b.upsertUser(from, refCode)
	if err != nil {
		b.log.Error("bot: cannot upsert user", "telegram_id", from.ID, "error", err)
		return
	}
	if user.Disabled {
		return
	}
	// Expired subscriptions are ignored the same way disabled accounts are:
	// no /start, no app button — silence (the Mini App shows the localized
	// "subscription expired" screen on sign-in; see docs/billing.md).
	if b.userExpired(user) {
		return
	}
	lang := pickLang(from.LanguageCode)

	switch firstWord(msg.Text) {
	case "/start":
		if refCode != "" {
			b.sendMessage(ctx, msg.Chat.ID, t(lang, "welcomeReferred"), b.miniAppKeyboard(lang, refCode, ""))
		} else {
			b.sendMessage(ctx, msg.Chat.ID, t(lang, "welcome"), b.miniAppKeyboard(lang, "", ""))
		}
	case "/help":
		b.sendMessage(ctx, msg.Chat.ID, t(lang, "help"), nil)
	case "/stats":
		if user.Role != "superadmin" {
			b.sendMessage(ctx, msg.Chat.ID, t(lang, "denied"), nil)
			return
		}
		ov, err := b.statsFn()
		if err != nil {
			b.sendMessage(ctx, msg.Chat.ID, t(lang, "statsError"), nil)
			return
		}
		b.sendMessage(ctx, msg.Chat.ID, formatStats(lang, ov), nil)
	case "/invite":
		if user.Role != "admin" && user.Role != "superadmin" {
			b.sendMessage(ctx, msg.Chat.ID, t(lang, "denied"), nil)
			return
		}
		code, err := api.GenerateInviteCode()
		if err != nil {
			b.sendMessage(ctx, msg.Chat.ID, t(lang, "inviteError"), nil)
			return
		}
		if err := b.store.CreateInvite(&store.Invite{Code: code, CreatedBy: user.ID}); err != nil {
			b.log.Warn("bot: cannot create invite", "error", err)
			b.sendMessage(ctx, msg.Chat.ID, t(lang, "inviteError"), nil)
			return
		}
		b.sendMessage(ctx, msg.Chat.ID, t(lang, "inviteCreated", code, api.InviteShareLink(b.cfg.Domain, b.me, code)), nil)
	case "/ref":
		b.sendMessage(ctx, msg.Chat.ID, t(lang, "referralCard", user.ReferralCode, api.ReferralShareLink(b.cfg.Domain, b.me, user.ReferralCode)), nil)
	case "/app":
		// Telegram-side APK delivery: in some regions (RU DPI) a direct
		// browser download from the VPS times out, while Telegram file
		// transfer is always fast. The slim arm64 build fits the 50MB bot
		// upload limit; sync it with install/sync-downloads.sh.
		dir := os.Getenv("FIBR_DOWNLOAD_DIR")
		if dir == "" {
			dir = "/download"
		}
		apk := filepath.Join(dir, "Fibr-Android.apk")
		if fi, err := os.Stat(apk); err == nil && !fi.IsDir() {
			b.sendMessage(ctx, msg.Chat.ID, t(lang, "appSending", fmt.Sprintf("%.0f", float64(fi.Size())/1048576)), nil)
			if err := b.sendDocumentFile(ctx, msg.Chat.ID, apk, "Fibr"); err != nil {
				b.log.Warn("bot: sendDocument failed", "error", err)
				b.sendMessage(ctx, msg.Chat.ID, t(lang, "appLinks", "https://github.com/fenjo26/Fibr/releases/latest"), nil)
			}
		} else {
			b.sendMessage(ctx, msg.Chat.ID, t(lang, "appLinks", "https://github.com/fenjo26/Fibr/releases/latest"), nil)
		}
	case "/id":
		b.sendMessage(ctx, msg.Chat.ID, "Telegram ID: "+strconv.FormatInt(from.ID, 10), nil)
	default:
		b.sendMessage(ctx, msg.Chat.ID, t(lang, "unknown"), nil)
	}
}

// startPayload splits a "/start <payload>" message into a referral code
// ("ref_CODE") or an invite code ("inv_CODE"); a plain /start or any other
// payload yields two empty strings.
func startPayload(text string) (refCode, inviteCode string) {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return "", ""
	}
	payload := fields[1]
	if rest, ok := strings.CutPrefix(payload, "ref_"); ok {
		return rest, ""
	}
	if rest, ok := strings.CutPrefix(payload, "inv_"); ok {
		return "", rest
	}
	return "", ""
}

// upsertUser mirrors the Mini App auth: FIBR_ADMIN_TELEGRAM_IDS entries are
// (re)promoted to superadmin on every message, everyone else registers as a
// plain user. A referral code presented on the very first message (the
// /start of a t.me deep link) records the attribution once, at account
// creation — the same one-shot semantics as the Mini App sign-in.
func (b *Bot) upsertUser(from *tgUser, refCode string) (*store.User, error) {
	if u, err := b.store.GetUserByTelegramID(from.ID); err == nil {
		if u.Role != "superadmin" && b.isAdminID(from.ID) {
			_ = b.store.SetUserRole(u.ID, "superadmin")
			u.Role = "superadmin"
		}
		return u, nil
	} else if err != store.ErrNotFound {
		return nil, err
	}
	role := "user"
	if b.isAdminID(from.ID) {
		role = "superadmin"
	}
	tid := from.ID
	name := from.Username
	if name == "" {
		name = from.FirstName
	}
	u := &store.User{TelegramID: &tid, Username: name, Role: role}
	if refCode != "" {
		// Unknown or disabled referrer: register without attribution rather
		// than bouncing a human at the door.
		if ref, err := b.store.GetUserByReferralCode(refCode); err == nil && !ref.Disabled {
			if ref.TelegramID == nil || *ref.TelegramID != tid {
				rid := ref.ID
				u.ReferredBy = &rid
				b.log.Info("bot: referral attribution", "new_user", tid, "referred_by", rid)
			}
		}
	}
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

// userExpired reports whether the account's effective subscription deadline
// (its own expires_at OR its owner-admin's, whichever is earlier) has passed.
func (b *Bot) userExpired(u *store.User) bool {
	var ownerExp *time.Time
	if u.OwnerID != nil {
		if owner, err := b.store.GetUser(*u.OwnerID); err == nil {
			ownerExp = owner.ExpiresAt
		}
	}
	return store.ExpiredAt(store.EffectiveExpiry(u.ExpiresAt, ownerExp), time.Now())
}

// miniAppKeyboard returns the inline button opening the Mini App. Web App
// buttons require an https URL, so they are skipped in local dev. A referral
// or invite code rides in the button URL (?ref=CODE / ?invite=CODE) so the
// Mini App sign-in sees it even though bot deep links cannot set startapp.
func (b *Bot) miniAppKeyboard(lang, refCode, inviteCode string) any {
	if b.cfg.Domain == "" || b.cfg.Domain == "localhost" || b.cfg.Domain == "127.0.0.1" {
		return nil
	}
	q := neturl.Values{}
	if refCode != "" {
		q.Set("ref", refCode)
	}
	if inviteCode != "" {
		q.Set("invite", inviteCode)
	}
	url := "https://" + b.cfg.Domain + "/miniapp/"
	if len(q) > 0 {
		url += "?" + q.Encode()
	}
	return map[string]any{
		"inline_keyboard": [][]map[string]any{{
			{"text": t(lang, "openApp"), "web_app": map[string]string{"url": url}},
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

// sendDocumentFile uploads a local file to a chat as a document (multipart).
// Used by /app to deliver the client APK over Telegram — in some regions a
// direct HTTPS download from the VPS is throttled, Telegram never is.
func (b *Bot) sendDocumentFile(ctx context.Context, chatID int64, path, caption string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	_ = w.WriteField("caption", caption)
	fw, err := w.CreateFormFile("document", filepath.Base(path))
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+b.token+"/sendDocument", body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if !out.OK {
		return fmt.Errorf("sendDocument: %s", out.Description)
	}
	return nil
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
		"welcome":         "👋 Добро пожаловать в <b>Fibr</b>!\n\nКнопкой ниже откройте приложение: там можно создать устройство, забрать QR-код конфигурации и посмотреть свою статистику.",
		"welcomeReferred": "👋 Добро пожаловать в <b>Fibr</b>!\n\nВы пришли по приглашению — оно уже учтено ✅\nОткройте приложение кнопкой ниже: создайте устройство и заберите QR-код конфигурации.",
		"help":            "Команды:\n/start — открыть приложение\n/help — справка\n/app — получить приложение (APK файлом)\n/ref — реферальный код и ссылка\n/stats — статистика (только владелец)\n/invite — создать приглашение (только админ)\n/id — узнать свой Telegram ID",
		"denied":          "⛔ Команда доступна только администраторам.",
		"unknown":         "Не понял команду. /help — список команд.",
		"appSending":      "📲 Отправляю приложение (~%s МБ)…",
		"appLinks":        "Скачать приложение:\n— у бота: отправьте /app (приложение придёт файлом)\n— с сервера: https://<ваш-домен>/app.apk\n— с GitHub: %s/latest",
		"inviteCreated":   "🎟 Одноразовое приглашение:\n\nКод: <code>%s</code>\nСсылка: %s\n\nОдин человек сможет зарегистрироваться, открыв ссылку в Telegram.",
		"inviteError":     "Не удалось создать приглашение, смотрите логи сервера.",
		"referralCard":    "🎁 Реферальная программа\n\nВаш код: <code>%s</code>\nСсылка: %s\n\nОтправьте их другу — с каждого его пополнения вам идёт процент. Баланс и подробности — в приложении.",
		"openApp":         "Открыть Fibr",
		"welcomeInvite":   "👋 Добро пожаловать в <b>Fibr</b>!\n\nНажмите кнопку ниже — приложение откроется и активирует ваше приглашение.",
		"inviteOnly":      "🔒 Регистрация в Fibr только по приглашению. Попросите ссылку-приглашение у того, кто вас позвал.",
		"statsHeader":     "📊 <b>Статистика Fibr</b>",
		"statsError":      "Не удалось собрать статистику, смотрите логи сервера.",
		"users":           "Пользователей",
		"devices":         "Устройств",
		"online":          "Онлайн",
		"expiredSubs":     "Истекших подписок",
		"traffic":         "Трафик",
		"tunnelRate":      "Туннель",
	},
	"en": {
		"welcome":         "👋 Welcome to <b>Fibr</b>!\n\nUse the button below to open the app: create a device, grab a QR config and check your traffic stats.",
		"welcomeReferred": "👋 Welcome to <b>Fibr</b>!\n\nYou arrived via an invitation — it has been credited ✅\nOpen the app with the button below: create a device and grab a QR config.",
		"help":            "Commands:\n/start — open the app\n/help — help\n/app — get the app (APK as a file)\n/ref — your referral code and link\n/stats — stats (owner only)\n/invite — create an invite (admin only)\n/id — show your Telegram ID",
		"denied":          "⛔ This command is admin-only.",
		"unknown":         "Unknown command. /help lists commands.",
		"appSending":      "📲 Sending the app (~%s MB)…",
		"appLinks":        "Get the app:\n— from this bot: send /app (comes as a file)\n— from the server: https://<your-domain>/app.apk\n— from GitHub: %s/latest",
		"inviteCreated":   "🎟 One-time invite:\n\nCode: <code>%s</code>\nLink: %s\n\nOne person can register by opening the link in Telegram.",
		"inviteError":     "Failed to create an invite, check server logs.",
		"referralCard":    "🎁 Referral program\n\nYour code: <code>%s</code>\nLink: %s\n\nShare them with a friend — you earn a cut of every top-up they make. Balance and details are in the app.",
		"openApp":         "Open Fibr",
		"welcomeInvite":   "👋 Welcome to <b>Fibr</b>!\n\nTap the button below — the app opens and activates your invitation.",
		"inviteOnly":      "🔒 Fibr is invite-only. Ask whoever told you about it for an invite link.",
		"statsHeader":     "📊 <b>Fibr stats</b>",
		"statsError":      "Failed to collect stats, check server logs.",
		"users":           "Users",
		"devices":         "Devices",
		"online":          "Online",
		"expiredSubs":     "Expired subscriptions",
		"traffic":         "Traffic",
		"tunnelRate":      "Tunnel",
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
	return fmt.Sprintf("%s\n%s: %d\n%s: %d\n%s: %d\n%s: %d\n%s: ↓%s ↑%s\n%s: ↓%s ↑%s",
		t(lang, "statsHeader"),
		t(lang, "users"), ov.Totals.Users,
		t(lang, "devices"), ov.Totals.Devices,
		t(lang, "online"), ov.Totals.Online,
		t(lang, "expiredSubs"), ov.Totals.Expired,
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
