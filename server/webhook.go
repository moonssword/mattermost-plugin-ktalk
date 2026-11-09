package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

const (
	webhookSecretHeader = "X-Konturtalk-Secret"
	webhookTitle        = "Mattermost"
	eventDedupTTL       = 24 * time.Hour
	activationWait      = 8 * time.Second
	maxWebhookCreates   = 10
)

// webhookState is the plugin's record of its registration.
type webhookState struct {
	WebhookKey string    `json:"webhook_key"`
	URL        string    `json:"url"`
	Activated  bool      `json:"activated"`
	CreatedAt  time.Time `json:"created_at"`
	LastEvent  time.Time `json:"last_event"`
}

// webhookEvent is a delivery from Talk.
type webhookEvent struct {
	ActivationKey string    `json:"activationKey"`
	EventID       string    `json:"eventId"`
	EventType     string    `json:"eventType"`
	Time          time.Time `json:"time"`
	ConferenceKey string    `json:"conferenceKey"`
	RoomName      string    `json:"roomName"`
	RecordingKey  string    `json:"recordingKey"`
}

func (p *Plugin) webhookTarget() string {
	config := p.API.GetConfig()
	if config == nil || config.ServiceSettings.SiteURL == nil || *config.ServiceSettings.SiteURL == "" {
		return ""
	}
	return strings.TrimRight(*config.ServiceSettings.SiteURL, "/") + "/plugins/" + pluginID + "/webhook"
}

// handleWebhook receives deliveries. It fails closed without a matching secret
// and answers at once, since Talk expects a reply within five seconds.
func (p *Plugin) handleWebhook(w http.ResponseWriter, r *http.Request) {
	secret, err := p.secret(secretWebhook)
	got := r.Header.Get(webhookSecretHeader)
	if err != nil || !p.getConfig().EnableWebhooks || secret == "" || subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var event webhookEvent
	if err := json.Unmarshal(data, &event); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)

	if event.ActivationKey != "" {
		if err := p.kvSet(keyActivate, event.ActivationKey, time.Hour); err != nil {
			p.API.LogError("Failed to store webhook activation key", "error", err.Error())
		}
		go p.activateWebhook()
		return
	}
	if event.EventID == "" {
		return
	}
	first, err := p.kvClaim(keyEvent+event.EventID, eventDedupTTL)
	if err != nil {
		p.API.LogWarn("Webhook dedup failed", "error", err.Error())
	}
	if err == nil && !first {
		return
	}
	p.metrics.webhookEvents.Add(1)
	go p.dispatchEvent(event)
}

// dispatchEvent routes an event to its meeting and syncs it. The sync reads the
// real state, so duplicates, reordering and reconnect noise do no harm.
func (p *Plugin) dispatchEvent(event webhookEvent) {
	p.touchWebhook()
	var meeting *Meeting
	var err error
	// The docs spell types both ways: conferencesStarted in the subscription
	// and ConferencesStarted in the delivery examples.
	switch strings.ToLower(event.EventType) {
	case "conferencesstarted":
		meeting, err = p.getMeetingByIndex(keyRoom + event.RoomName)
		if err == nil && event.ConferenceKey != "" {
			if setErr := p.kvSet(keyConf+event.ConferenceKey, meeting.ID, finishedKeepFor); setErr != nil {
				p.API.LogWarn("Conference index failed", "error", setErr.Error())
			}
			_, _, _ = p.updateMeeting(meeting.ID, func(m *Meeting) bool {
				m.ConferenceKey = event.ConferenceKey
				return true
			})
		}
	case "conferencesfinished":
		meeting, err = p.getMeetingByIndex(keyConf + event.ConferenceKey)
	case "userconnectedtoroom", "userdisconnectedfromroom":
		meeting, err = p.getMeetingByIndex(keyRoom + event.RoomName)
		if err == nil {
			p.syncSoon(meeting.ID)
		}
		return
	case "recordingcompleted", "transcriptionready":
		meeting, err = p.meetingForRecording(event.RecordingKey)
		if err == nil {
			// An event means there is news, so skip the polling pause.
			_, _, _ = p.updateMeeting(meeting.ID, func(m *Meeting) bool {
				m.ArtifactPollAt = time.Time{}
				return true
			})
		}
	default:
		return
	}
	if err != nil {
		if !errors.Is(err, errMeetingNotFound) {
			p.API.LogWarn("Webhook event not routed", "type", event.EventType, "error", err.Error())
		}
		return
	}
	p.syncMeetingByID(meeting.ID)
}

func (p *Plugin) meetingForRecording(key string) (*Meeting, error) {
	if m, err := p.getMeetingByIndex(keyRec + key); err == nil {
		return m, nil
	}
	client := p.client()
	if client == nil {
		return nil, errNotConfigured
	}
	ctx, cancel := apiContext()
	defer cancel()
	rec, err := client.GetRecording(ctx, key)
	if err != nil {
		return nil, err
	}
	return p.getMeetingByIndex(keyRoom + rec.RoomName)
}

func (p *Plugin) touchWebhook() {
	var state webhookState
	found, err := p.kvGet(keyWebhook, &state)
	if err != nil || !found {
		return
	}
	state.LastEvent = time.Now().UTC()
	if err := p.kvSet(keyWebhook, state, 0); err != nil {
		p.API.LogDebug("Failed to record webhook time", "error", err.Error())
	}
}

// registerWebhook reuses a matching active registration, because creating one
// is limited to ten calls a day for the whole space.
func (p *Plugin) registerWebhook() (*webhookState, error) {
	client, err := p.requireClient()
	if err != nil {
		return nil, err
	}
	cfg := p.getConfig()
	if !cfg.EnableWebhooks {
		return nil, newUserError(http.StatusConflict, "Сначала включите вебхуки в настройках плагина и сохраните их.")
	}
	target := p.webhookTarget()
	if target == "" {
		return nil, newUserError(http.StatusConflict, "В Mattermost не задан Site URL.")
	}
	ctx, cancel := apiContext()
	defer cancel()
	existing, err := client.ListWebhooks(ctx)
	if err != nil {
		if talk.IsStatus(err, http.StatusForbidden) {
			return nil, newUserError(http.StatusBadGateway, "У API-ключа нет области «Вебхуки». Пока работает опрос API. Запросите область у администратора Толка.")
		}
		return nil, p.describeAPIError("list webhooks", err)
	}
	for i := range existing {
		if existing[i].URL != target {
			continue
		}
		state := webhookState{WebhookKey: existing[i].WebhookKey, URL: target, Activated: existing[i].Activated, CreatedAt: time.Now().UTC()}
		if err := p.kvSet(keyWebhook, state, 0); err != nil {
			return nil, err
		}
		if !state.Activated {
			go p.activateWebhook()
		}
		return &state, nil
	}

	secret, err := p.secret(secretWebhook)
	if err != nil {
		return nil, err
	}
	created, err := client.CreateWebhook(ctx, webhookTitle, target, []talk.WebhookHeader{{Name: webhookSecretHeader, Value: secret}})
	if err != nil {
		if talk.IsStatus(err, http.StatusTooManyRequests) {
			return nil, newUserError(http.StatusTooManyRequests, fmt.Sprintf("Толк позволяет создавать не более %d вебхуков в сутки. Повторите завтра.", maxWebhookCreates))
		}
		return nil, p.describeAPIError("create webhook", err)
	}
	state := webhookState{WebhookKey: created.WebhookKey, URL: target, Activated: created.Activated, CreatedAt: time.Now().UTC()}
	if err := p.kvSet(keyWebhook, state, 0); err != nil {
		return nil, err
	}
	go p.activateWebhook()
	return &state, nil
}

// activateWebhook finishes registration once both halves have arrived: the
// webhook key from the create response and the key delivered to our URL. They
// can arrive in either order, so this waits briefly for the other half.
func (p *Plugin) activateWebhook() {
	client := p.client()
	if client == nil {
		return
	}
	deadline := time.Now().Add(activationWait)
	for {
		var state webhookState
		var activation string
		stateFound, stateErr := p.kvGet(keyWebhook, &state)
		keyFound, keyErr := p.kvGet(keyActivate, &activation)
		if stateErr == nil && keyErr == nil && stateFound && keyFound {
			if state.Activated {
				return
			}
			ctx, cancel := apiContext()
			err := client.ActivateWebhook(ctx, state.WebhookKey, activation)
			cancel()
			if err != nil {
				p.noteAPIError(err)
				p.API.LogError("Webhook activation failed", "error", err.Error())
				return
			}
			state.Activated = true
			if err := p.kvSet(keyWebhook, state, 0); err != nil {
				p.API.LogError("Failed to save webhook state", "error", err.Error())
			}
			if appErr := p.API.KVDelete(keyActivate); appErr != nil {
				p.API.LogWarn("Failed to clear activation key", "error", appErr.Error())
			}
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// removeWebhook deletes the registration in Talk and locally.
func (p *Plugin) removeWebhook() error {
	client, err := p.requireClient()
	if err != nil {
		return err
	}
	var state webhookState
	found, err := p.kvGet(keyWebhook, &state)
	if err != nil {
		return err
	}
	if found {
		ctx, cancel := apiContext()
		defer cancel()
		if err := client.DeleteWebhook(ctx, state.WebhookKey); err != nil && !talk.IsNotFound(err) {
			return p.describeAPIError("delete webhook", err)
		}
	}
	if appErr := p.API.KVDelete(keyWebhook); appErr != nil {
		return appErr
	}
	return nil
}

// forgetWebhook runs after the space or key changed, for example on the move
// from a test space to production. It deletes the old registration with the
// old credentials and leaves registration of the new one to the administrator.
func (p *Plugin) forgetWebhook(previous *configuration) {
	var state webhookState
	found, err := p.kvGet(keyWebhook, &state)
	if err != nil || !found {
		return
	}
	if previous.spaceURL() != "" && previous.APIKey != "" {
		ctx, cancel := apiContext()
		defer cancel()
		err := talk.NewClient(previous.spaceURL(), previous.APIKey).DeleteWebhook(ctx, state.WebhookKey)
		if err != nil && !talk.IsNotFound(err) {
			p.API.LogWarn("Failed to delete previous webhook", "error", err.Error())
		}
	}
	if appErr := p.API.KVDelete(keyWebhook); appErr != nil {
		p.API.LogWarn("Failed to clear webhook state", "error", appErr.Error())
	}
}
