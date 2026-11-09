package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

const (
	pluginID       = "konturtalk"
	botUsername    = "konturtalk"
	postTypeCard   = "custom_konturtalk_meeting"
	postTypeRec    = "custom_konturtalk_rec"
	wsMeeting      = "meeting_updated"
	wsConfig       = "config_changed"
	wsIncomingCall = "incoming_call"
	wsDismissCall  = "call_dismissed"
	prefCategory   = "pp_konturtalk"

	secretEncryption = "encryption"
	secretWebhook    = "webhook"
)

var errNotConfigured = errors.New("space address is not configured")

// Plugin is the server half of the Kontur Talk integration.
type Plugin struct {
	plugin.MattermostPlugin

	configLock sync.RWMutex
	config     *configuration
	router     http.Handler
	botUserID  string
	jobs       []*cluster.Job
	metrics    metrics

	debounceLock sync.Mutex
	debounced    map[string]*time.Timer
}

// metrics are per-node counters served by ServeMetrics.
type metrics struct {
	meetingsCreated atomic.Int64
	callsStarted    atomic.Int64
	apiErrors       atomic.Int64
	webhookEvents   atomic.Int64
	syncRuns        atomic.Int64
}

func (p *Plugin) getConfig() *configuration {
	p.configLock.RLock()
	defer p.configLock.RUnlock()
	if p.config == nil {
		return &configuration{}
	}
	return p.config
}

// OnConfigurationChange loads settings. An invalid address is logged and kept
// out, so a typo in the console cannot take the plugin down.
func (p *Plugin) OnConfigurationChange() error {
	cfg := &configuration{}
	if err := p.API.LoadPluginConfiguration(cfg); err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		p.API.LogError("Invalid Kontur Talk settings", "error", err.Error())
		cfg.SpaceURL = ""
	}

	p.configLock.Lock()
	previous := p.config
	p.config = cfg
	p.configLock.Unlock()

	if previous != nil && p.botUserID != "" {
		if connectionChanged(previous, cfg) {
			// The old registration points at the old space or address.
			go p.forgetWebhook(previous)
		}
		// Open clients apply a new default style and dev mode list at once.
		p.API.PublishWebSocketEvent(wsConfig, map[string]any{}, &model.WebsocketBroadcast{})
	}
	return nil
}

func connectionChanged(a, b *configuration) bool {
	return a.spaceURL() != b.spaceURL() || a.APIKey != b.APIKey
}

// OnActivate prepares the bot, command and background jobs.
func (p *Plugin) OnActivate() error {
	botID, err := p.API.EnsureBotUser(&model.Bot{
		Username:    botUsername,
		DisplayName: "Контур.Толк",
		Description: "Встречи, записи и резюме Контур.Толка.",
	})
	if err != nil {
		return fmt.Errorf("ensure bot: %w", err)
	}
	p.botUserID = botID
	p.setBotIcon(botID)

	p.debounced = map[string]*time.Timer{}
	p.router = p.initRouter()

	if err := p.API.RegisterCommand(talkCommand()); err != nil {
		return fmt.Errorf("register command: %w", err)
	}

	syncJob, err := cluster.Schedule(p.API, "konturtalk_sync", cluster.MakeWaitForInterval(syncInterval), p.runSync)
	if err != nil {
		return fmt.Errorf("schedule sync: %w", err)
	}
	alertJob, err := cluster.Schedule(p.API, "konturtalk_alerts", cluster.MakeWaitForInterval(alertInterval), p.runAlerts)
	if err != nil {
		_ = syncJob.Close()
		return fmt.Errorf("schedule alerts: %w", err)
	}
	p.jobs = []*cluster.Job{syncJob, alertJob}
	return nil
}

// OnDeactivate stops background jobs and pending debounced syncs.
//
//nolint:unparam // Signature of plugin.Hooks.
func (p *Plugin) OnDeactivate() error {
	for _, job := range p.jobs {
		if err := job.Close(); err != nil {
			p.API.LogWarn("Failed to close job", "error", err.Error())
		}
	}
	p.debounceLock.Lock()
	for _, timer := range p.debounced {
		timer.Stop()
	}
	p.debounced = map[string]*time.Timer{}
	p.debounceLock.Unlock()
	return nil
}

// secret returns a generated secret, creating it on first use. It lives in the
// plugin KV store: writing server configuration during activation races with
// the plugin enable call, which saves the configuration too. The atomic write
// makes all cluster nodes settle on one value.
func (p *Plugin) secret(name string) (string, error) {
	key := keySecret + name
	var value string
	found, err := p.kvGet(key, &value)
	if err != nil {
		return "", err
	}
	if found && value != "" {
		return value, nil
	}
	generated, err := generateSecret()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(generated)
	if err != nil {
		return "", err
	}
	if _, appErr := p.API.KVSetWithOptions(key, data, model.PluginKVSetOptions{Atomic: true, OldValue: nil}); appErr != nil {
		return "", fmt.Errorf("store secret: %w", appErr)
	}
	if _, err := p.kvGet(key, &value); err != nil {
		return "", err
	}
	if value == "" {
		return "", errors.New("secret was not stored")
	}
	return value, nil
}

func (p *Plugin) setBotIcon(botID string) {
	bundle, err := p.API.GetBundlePath()
	if err != nil {
		p.API.LogWarn("Bundle path unavailable", "error", err.Error())
		return
	}
	data, err := os.ReadFile(filepath.Join(bundle, "assets", "bot.png"))
	if err != nil {
		p.API.LogWarn("Bot icon unavailable", "error", err.Error())
		return
	}
	if appErr := p.API.SetProfileImage(botID, data); appErr != nil {
		p.API.LogWarn("Failed to set bot icon", "error", appErr.Error())
	}
}

// userAllowed applies test mode. It fails closed if the user cannot be loaded.
func (p *Plugin) userAllowed(userID string) bool {
	cfg := p.getConfig()
	if !cfg.TestMode {
		return true
	}
	user, appErr := p.API.GetUser(userID)
	return appErr == nil && cfg.allowsUser(user)
}

// client returns the space-key client, nil in link-only mode.
func (p *Plugin) client() *talk.Client {
	cfg := p.getConfig()
	if cfg.spaceURL() == "" || cfg.APIKey == "" {
		return nil
	}
	return talk.NewClient(cfg.spaceURL(), cfg.APIKey)
}

func apiContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 25*time.Second)
}

// noteAPIError counts an API failure and raises the auth alert when due.
func (p *Plugin) noteAPIError(err error) {
	if err == nil {
		return
	}
	p.metrics.apiErrors.Add(1)
	if talk.IsStatus(err, http.StatusUnauthorized) {
		p.alertAdmins("auth", 24*time.Hour, "Контур.Толк отклоняет API-ключ (HTTP 401). Проверьте ключ в **Системная консоль → Плагины → Контур.Толк**.")
	}
}

// --- Personal key encryption ---

func (p *Plugin) gcm() (cipher.AEAD, error) {
	secret, err := p.secret(secretEncryption)
	if err != nil {
		return nil, err
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (p *Plugin) encrypt(plain string) (string, error) {
	gcm, err := p.gcm()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (p *Plugin) decrypt(encoded string) (string, error) {
	gcm, err := p.gcm()
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// ServeMetrics exposes per-node counters in the OpenMetrics text format.
func (p *Plugin) ServeMetrics(_ *plugin.Context, w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	for name, value := range map[string]int64{
		"konturtalk_meetings_created_total": p.metrics.meetingsCreated.Load(),
		"konturtalk_calls_started_total":    p.metrics.callsStarted.Load(),
		"konturtalk_api_errors_total":       p.metrics.apiErrors.Load(),
		"konturtalk_webhook_events_total":   p.metrics.webhookEvents.Load(),
		"konturtalk_sync_runs_total":        p.metrics.syncRuns.Load(),
	} {
		_, _ = fmt.Fprintf(w, "# TYPE %s counter\n%s %d\n", name, name, value)
	}
}

// publishConfigChanged tells one user's open tabs to reload the plugin
// settings that depend on them, such as a connected personal key.
func (p *Plugin) publishConfigChanged(userID string) {
	p.API.PublishWebSocketEvent(wsConfig, map[string]any{}, &model.WebsocketBroadcast{UserId: userID})
}
