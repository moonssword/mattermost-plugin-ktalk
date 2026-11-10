package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

const maxBodyBytes = 1 << 20

// ServeHTTP routes plugin requests.
func (p *Plugin) ServeHTTP(_ *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

func (p *Plugin) initRouter() http.Handler {
	mux := http.NewServeMux()
	// The webhook authenticates itself with its secret; everything else
	// requires a logged-in Mattermost user.
	mux.HandleFunc("POST /webhook", p.handleWebhook)
	// The status emoji picture, which the webapp registers as a custom emoji.
	mux.HandleFunc("GET /status-emoji.png", p.serveStatusEmoji)

	// Test mode closes everything below to users outside it. Clients read
	// the config to learn that, and administrators need the console to change it.
	open := func(pattern string, handler func(http.ResponseWriter, *http.Request, string) error) {
		mux.Handle(pattern, p.authenticated(handler))
	}
	route := func(pattern string, handler func(http.ResponseWriter, *http.Request, string) error) {
		mux.Handle(pattern, p.authenticated(p.testModeGate(handler)))
	}
	open("GET /api/v1/config", p.apiConfig)
	open("GET /api/v1/admin/status", p.apiAdminStatus)
	open("POST /api/v1/admin/webhook", p.apiAdminRegisterWebhook)
	open("DELETE /api/v1/admin/webhook", p.apiAdminDeleteWebhook)
	route("POST /api/v1/meetings", p.apiCreateMeeting)
	route("GET /api/v1/meetings/active", p.apiActiveMeetings)
	route("POST /api/v1/meetings/{id}/{action}", p.apiMeetingAction)
	route("GET /api/v1/meetings/{id}/rights", p.apiMeetingRights)
	route("POST /api/v1/calls", p.apiCreateCall)
	route("GET /api/v1/personal-room", p.apiPersonalRoom)
	route("GET /api/v1/upcoming", p.apiUpcoming)
	route("POST /api/v1/seen", p.apiSeen)
	route("GET /api/v1/personal-key", p.apiPersonalKeyStatus)
	route("POST /api/v1/personal-key", p.apiConnectPersonalKey)
	route("DELETE /api/v1/personal-key", p.apiDisconnectPersonalKey)
	route("GET /api/v1/recordings", p.apiRecordings)
	route("GET /api/v1/recordings/{key}/artifacts", p.apiRecordingArtifacts)
	route("POST /api/v1/recordings/access", p.apiRecordingsAccess)
	route("GET /api/v1/users/{id}/status", p.apiUserStatus)
	route("GET /api/v1/preview", p.apiPreview)
	route("POST /dialogs/start", p.dialogStart)
	route("POST /dialogs/connect", p.dialogConnect)
	route("POST /dialogs/schedule", p.dialogSchedule)
	route("POST /dialogs/users", p.dialogUsers)
	return mux
}

const testModeMessage = "Плагин работает в режиме тестирования: пока им пользуются только тестировщики."

func (p *Plugin) testModeGate(handler func(http.ResponseWriter, *http.Request, string) error) func(http.ResponseWriter, *http.Request, string) error {
	return func(w http.ResponseWriter, r *http.Request, userID string) error {
		if !p.userAllowed(userID) {
			return newUserError(http.StatusForbidden, testModeMessage)
		}
		return handler(w, r, userID)
	}
}

func (p *Plugin) serveStatusEmoji(w http.ResponseWriter, _ *http.Request) {
	bundle, err := p.API.GetBundlePath()
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, &http.Request{Method: http.MethodGet, Header: http.Header{}, URL: &url.URL{}}, filepath.Join(bundle, "assets", "status.png"))
}

func (p *Plugin) authenticated(handler func(http.ResponseWriter, *http.Request, string) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-Id")
		if userID == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Требуется вход в Mattermost."})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		if err := handler(w, r, userID); err != nil {
			var known *userError
			if errors.As(err, &known) {
				writeJSON(w, known.status, map[string]string{"error": known.message, "code": known.code})
				return
			}
			p.API.LogError("Request failed", "path", r.URL.Path, "error", err.Error())
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Внутренняя ошибка плагина."})
		}
	})
}

func decodeBody(r *http.Request, out any) error {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return newUserError(http.StatusBadRequest, "Не удалось прочитать запрос.")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return newUserError(http.StatusBadRequest, "Неверный формат запроса.")
	}
	return nil
}

func (p *Plugin) apiConfig(w http.ResponseWriter, _ *http.Request, userID string) error {
	cfg := p.getConfig()
	var stored personalKey
	connected, _ := p.kvGet(keyPAT+userID, &stored)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":          p.userAllowed(userID),
		"test_mode":        cfg.TestMode,
		"style":            cfg.CardStyle,
		"bot_user_id":      p.botUserID,
		"configured":       cfg.spaceURL() != "",
		"space_url":        cfg.spaceURL(),
		"link_only":        p.client() == nil,
		"naming":           p.userPref(userID, prefNaming, cfg.NamingScheme),
		"open_mode":        p.userPref(userID, prefOpenMode, "tab"),
		"calls":            cfg.EnableCalls,
		"personal_key":     connected,
		"join_opens_min":   int(joinOpensBefore.Minutes()),
		"recording_access": cfg.defaultRecordingAccess(),
	})
	return nil
}

func (p *Plugin) meetingResponse(w http.ResponseWriter, m *Meeting) {
	writeJSON(w, http.StatusOK, map[string]any{"meeting": m.view(), "url": m.URL})
}

func (p *Plugin) apiCreateMeeting(w http.ResponseWriter, r *http.Request, userID string) error {
	var req createRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	if req.ChannelID == "" && req.PostID == "" {
		return newUserError(http.StatusBadRequest, "Не указан канал.")
	}
	m, err := p.startMeeting(userID, req)
	if err != nil {
		return err
	}
	p.meetingResponse(w, m)
	return nil
}

// apiMeetingRights tells a card what to offer the user, as Talk sees them.
// Only members of the meeting's channel may ask: that is where the card is.
func (p *Plugin) apiMeetingRights(w http.ResponseWriter, r *http.Request, userID string) error {
	m, err := p.getMeeting(r.PathValue("id"))
	if errors.Is(err, errMeetingNotFound) {
		return newUserError(http.StatusNotFound, "Встреча не найдена.")
	}
	if err != nil {
		return err
	}
	if !p.canView(userID, m) {
		return errForbidden
	}
	writeJSON(w, http.StatusOK, p.meetingRights(userID, m))
	return nil
}

func (p *Plugin) apiCreateCall(w http.ResponseWriter, r *http.Request, userID string) error {
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	m, err := p.startDirectCall(userID, req.UserID)
	if err != nil {
		return err
	}
	p.meetingResponse(w, m)
	return nil
}

func (p *Plugin) apiMeetingAction(w http.ResponseWriter, r *http.Request, userID string) error {
	id := r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "end":
		err = p.endMeeting(userID, id)
	case "record":
		var body struct {
			Access string `json:"access"`
		}
		if r.ContentLength > 0 {
			if err := decodeBody(r, &body); err != nil {
				return err
			}
		}
		err = p.startRecording(userID, id, body.Access)
	case "cancel-call":
		err = p.cancelRinging(userID, id)
	case "cancel-schedule":
		err = p.cancelSchedule(userID, id)
	case "answer":
		err = p.answerCall(userID, id, true)
	case "decline":
		err = p.answerCall(userID, id, false)
	case "accept-invite":
		err = p.respondInvitation(userID, id, true)
	case "decline-invite":
		err = p.respondInvitation(userID, id, false)
	default:
		return newUserError(http.StatusNotFound, "Неизвестное действие.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}

// apiActiveMeetings lists open meetings in channels the user can read.
func (p *Plugin) apiActiveMeetings(w http.ResponseWriter, _ *http.Request, userID string) error {
	meetings, err := p.activeMeetings()
	if err != nil {
		return err
	}
	now := time.Now()
	out := make([]map[string]any, 0, len(meetings))
	for _, m := range meetings {
		if (m.Status != statusLive && m.Status != statusWaiting && m.Status != statusRinging) || m.idle(now) || !p.canView(userID, m) {
			continue
		}
		view := m.view()
		if channel, appErr := p.API.GetChannel(m.ChannelID); appErr == nil {
			view["channel_name"] = channel.DisplayName
		}
		out = append(out, view)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (p *Plugin) apiPersonalRoom(w http.ResponseWriter, _ *http.Request, userID string) error {
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return appErr
	}
	link, err := p.personalRoom(user)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": link})
	return nil
}

func (p *Plugin) apiUpcoming(w http.ResponseWriter, _ *http.Request, userID string) error {
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return appErr
	}
	events, err := p.upcomingForUser(user)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, events)
	return nil
}

type recordingView struct {
	Key          string    `json:"key"`
	Title        string    `json:"title"`
	URL          string    `json:"url"`
	CreatedDate  time.Time `json:"created_date"`
	Duration     int       `json:"duration"`
	Participants int       `json:"participants"`
}

// maxRecordingPeriod caps how long a period one recordings request covers, so
// a search does not page through the whole history of the space. The web app
// allows 31 whole days; one more absorbs clock changes in the user's zone.
const maxRecordingPeriod = 32 * 24 * time.Hour

// recordingPeriod reads from and to (RFC 3339) and defaults to the last 30
// days. A longer period is refused rather than cut, so the user sees why.
func recordingPeriod(query url.Values, now time.Time) (from, to time.Time, err error) {
	to = now
	if raw := query.Get("to"); raw != "" {
		if to, err = time.Parse(time.RFC3339, raw); err != nil {
			return from, to, newUserError(http.StatusBadRequest, "Неверная дата окончания периода.")
		}
	}
	from = to.Add(-30 * 24 * time.Hour)
	if raw := query.Get("from"); raw != "" {
		if from, err = time.Parse(time.RFC3339, raw); err != nil {
			return from, to, newUserError(http.StatusBadRequest, "Неверная дата начала периода.")
		}
	}
	if !from.Before(to) {
		return from, to, newUserError(http.StatusBadRequest, "Начало периода должно быть раньше конца.")
	}
	if to.Sub(from) > maxRecordingPeriod {
		return from, to, newUserError(http.StatusBadRequest, "Период не может быть длиннее месяца.")
	}
	return from, to, nil
}

// Paging of the recordings list: Talk's own list of the user's recordings.
const (
	// recordingsWanted is how many recordings one answer aims to hold.
	recordingsWanted = 20
	userPageSize     = 50
	userPages        = 6
	// userPagePrefix marks a page token of the list.
	userPagePrefix = "u:"
)

// apiRecordings lists the recordings the user may open in a period. Talk's
// own list for the user, read with their personal key, says which recordings
// are theirs; the grants on each still decide, as everywhere else.
func (p *Plugin) apiRecordings(w http.ResponseWriter, r *http.Request, userID string) error {
	from, to, err := recordingPeriod(r.URL.Query(), time.Now())
	if err != nil {
		return err
	}
	personal, err := p.userClient(userID)
	if err != nil {
		return err
	}
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return appErr
	}
	talkKey, err := p.recordingViewer(user)
	if err != nil {
		return err
	}
	skip, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Query().Get("page"), userPagePrefix))
	ctx, cancel := apiContext()
	defer cancel()
	recs, next, err := p.userRecordings(ctx, personal, p.client(), talkKey, r.URL.Query().Get("q"), from, to, max(skip, 0))
	if err != nil {
		return p.personalAPIError(personal, userID, "list recordings", err)
	}
	cfg := p.getConfig()
	out := make([]recordingView, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordingView{Key: rec.Key, Title: rec.Title, URL: cfg.recordingURL(rec.Key), CreatedDate: rec.CreatedDate, Duration: rec.Duration, Participants: rec.ParticipantsCount})
	}
	writeJSON(w, http.StatusOK, map[string]any{"recordings": out, "next_page": next})
	return nil
}

// userRecordings pages through the list Talk shows the owner of a personal
// key. That list has no date filter, but it runs newest first, so paging
// stops at the first recording older than the period.
func (p *Plugin) userRecordings(ctx context.Context, personal, client *talk.Client, talkKey, query string, from, to time.Time, skip int) ([]talk.Recording, string, error) {
	var found []talk.Recording
	more := true
	for n := 0; n < userPages && more && len(found) < recordingsWanted; n++ {
		batch, err := personal.ListUserRecordings(ctx, query, skip, userPageSize)
		if err != nil {
			return nil, "", err
		}
		skip += len(batch)
		more = len(batch) == userPageSize
		for i := range batch {
			created := batch[i].CreatedDate
			if created.Before(from) {
				more = false
				break
			}
			if created.Before(to) {
				found = append(found, batch[i])
			}
		}
	}
	out := p.accessibleRecordings(ctx, client, talkKey, found)
	if out == nil {
		out = []talk.Recording{}
	}
	next := ""
	if more {
		next = userPagePrefix + strconv.Itoa(skip)
	}
	return out, next, nil
}

// maxAccessKeys bounds one access question: a card holds a few recordings.
const maxAccessKeys = 20

// apiRecordingsAccess tells which of the given recordings the user may open,
// so cards show buttons only to them. Any doubt answers no.
func (p *Plugin) apiRecordingsAccess(w http.ResponseWriter, r *http.Request, userID string) error {
	var req struct {
		Keys []string `json:"keys"`
	}
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	if len(req.Keys) > maxAccessKeys {
		return newUserError(http.StatusBadRequest, "Слишком много записей в одном запросе.")
	}
	allowed := make(map[string]bool, len(req.Keys))
	for _, key := range req.Keys {
		allowed[key] = false
	}
	client := p.client()
	user, appErr := p.API.GetUser(userID)
	if client == nil || appErr != nil {
		writeJSON(w, http.StatusOK, map[string]any{"access": allowed})
		return nil
	}
	talkKey, err := p.recordingViewer(user)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"access": allowed})
		return nil
	}
	ctx, cancel := apiContext()
	defer cancel()
	recs := make([]talk.Recording, 0, len(req.Keys))
	for _, key := range req.Keys {
		if key != "" {
			recs = append(recs, talk.Recording{Key: key})
		}
	}
	for _, rec := range p.accessibleRecordings(ctx, client, talkKey, recs) {
		allowed[rec.Key] = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"access": allowed})
	return nil
}

func (p *Plugin) apiRecordingArtifacts(w http.ResponseWriter, r *http.Request, userID string) error {
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return appErr
	}
	key := r.PathValue("key")
	// The texts are read as the user. Talk serves them to any user of the
	// space, so the grants on the recording are checked first.
	personal, err := p.userClient(userID)
	if err != nil {
		return err
	}
	rec, err := p.recordingForUser(user, key)
	if err != nil {
		return err
	}
	ctx, cancel := apiContext()
	defer cancel()
	artifacts, err := personal.GetSpeechArtifacts(ctx, key)
	if err != nil {
		return p.personalAPIError(personal, userID, "speech artifacts", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"title":      rec.Title,
		"url":        p.getConfig().recordingURL(key),
		"summary":    summaryView(artifacts.ShortSummary),
		"protocol":   summaryView(artifacts.Protocol),
		"transcript": transcriptView(artifacts.Transcript),
	})
	return nil
}

func summaryView(s *talk.Summary) map[string]string {
	if s == nil {
		return map[string]string{"status": "notFound", "text": ""}
	}
	return map[string]string{"status": s.Status, "text": s.Text()}
}

type transcriptLine struct {
	Speaker string `json:"speaker"`
	Start   int    `json:"start"`
	Text    string `json:"text"`
}

// transcriptView merges speaker tracks into one timeline.
func transcriptView(t *talk.Transcript) map[string]any {
	if t == nil {
		return map[string]any{"status": "notFound", "lines": []transcriptLine{}}
	}
	lines := []transcriptLine{}
	for i := range t.Tracks {
		speaker := ""
		if t.Tracks[i].Speaker != nil {
			speaker = t.Tracks[i].Speaker.Name()
		}
		for _, chunk := range t.Tracks[i].Chunks {
			lines = append(lines, transcriptLine{Speaker: speaker, Start: chunk.StartMillis, Text: chunk.Text})
		}
	}
	sortTranscript(lines)
	return map[string]any{"status": t.Status, "lines": lines}
}

func (p *Plugin) apiUserStatus(w http.ResponseWriter, r *http.Request, _ string) error {
	target, appErr := p.API.GetUser(r.PathValue("id"))
	if appErr != nil {
		return newUserError(http.StatusNotFound, "Пользователь не найден.")
	}
	status, err := p.talkStatus(target)
	if err != nil {
		// A status line is decoration: never fail the profile card over it.
		writeJSON(w, http.StatusOK, map[string]string{"status": ""})
		return nil
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
	return nil
}

var recordingPath = regexp.MustCompile(`^/recordings/([^/]+)$`)

// apiPreview describes a Talk link for an embed. Details come only from
// meetings the user may see: the space key can read any room, and link text
// typed by anyone must not become a way to probe them.
func (p *Plugin) apiPreview(w http.ResponseWriter, r *http.Request, userID string) error {
	cfg := p.getConfig()
	target, err := url.Parse(r.URL.Query().Get("url"))
	if err != nil || cfg.spaceURL() == "" || !strings.EqualFold(target.Scheme+"://"+target.Host, cfg.spaceURL()) {
		return newUserError(http.StatusBadRequest, "Это не ссылка на пространство Толка.")
	}
	if match := recordingPath.FindStringSubmatch(target.Path); match != nil {
		preview := map[string]any{"type": "recording", "url": target.String()}
		if user, appErr := p.API.GetUser(userID); appErr == nil && p.client() != nil {
			if rec, recErr := p.recordingForUser(user, match[1]); recErr == nil {
				preview["title"] = rec.Title
				preview["duration"] = rec.Duration
				preview["key"] = rec.Key
			}
		}
		writeJSON(w, http.StatusOK, preview)
		return nil
	}
	roomName := strings.Trim(target.Path, "/")
	preview := map[string]any{"type": "room", "url": target.String()}
	if validRoomName(roomName) {
		if m, indexErr := p.getMeetingByIndex(keyRoom + roomName); indexErr == nil && p.canView(userID, m) {
			preview["title"] = m.Title
			preview["users_online"] = m.UsersOnline
			preview["status"] = m.Status
		}
	}
	writeJSON(w, http.StatusOK, preview)
	return nil
}

// scopeFeatures maps API key scopes to what they enable, for the console.
var scopeFeatures = []struct {
	Scope   string
	Write   bool
	Feature string
}{
	{"rooms", true, "Комнаты, ПИН-код, зал ожидания, звонки"},
	{"profiles", false, "Сопоставление пользователей, статусы"},
	{"calendar", true, "Планирование встреч"},
	{"recording", false, "Записи, транскрипт и резюме"},
	{"activeRecordings", true, "Кнопка «Начать запись»"},
	{"reporting", false, "Итоги встречи, участники"},
	{"webhooks", true, "Мгновенные статусы (без неё работает опрос)"},
	{"onlineStats", false, "Счётчики и срок тарифа в консоли"},
}

func (p *Plugin) apiAdminStatus(w http.ResponseWriter, _ *http.Request, userID string) error {
	if !p.API.HasPermissionTo(userID, model.PermissionManageSystem) {
		return errForbidden
	}
	cfg := p.getConfig()
	status := map[string]any{"configured": cfg.spaceURL() != "", "space_url": cfg.spaceURL(), "has_key": cfg.APIKey != "", "webhooks_enabled": cfg.EnableWebhooks, "webhook_target": p.webhookTarget()}
	if cfg.TestMode {
		status["test_mode"] = p.testModeReport(cfg)
	}

	var state webhookState
	if found, err := p.kvGet(keyWebhook, &state); err == nil && found {
		status["webhook"] = state
	}
	if withKey, users, err := p.keyUsage(); err == nil {
		status["personal_keys"] = map[string]int{"connected": withKey, "users": users}
	}

	client := p.client()
	if client == nil {
		writeJSON(w, http.StatusOK, status)
		return nil
	}
	ctx, cancel := apiContext()
	defer cancel()
	info, err := client.GetAccessInfo(ctx)
	if err != nil {
		p.noteAPIError(err)
		status["key_error"] = keyErrorText(err)
		writeJSON(w, http.StatusOK, status)
		return nil
	}
	status["expires_at"] = info.ExpiredAt
	features := make([]map[string]any, 0, len(scopeFeatures))
	for _, item := range scopeFeatures {
		ok := info.CanRead(item.Scope)
		if item.Write {
			ok = info.CanWrite(item.Scope)
		}
		features = append(features, map[string]any{"scope": item.Scope, "feature": item.Feature, "ok": ok})
	}
	status["features"] = features

	if info.CanRead("onlineStats") {
		if online, err := client.GetOnlineStats(ctx); err == nil {
			status["online"] = online
		}
		if tariff, err := client.GetTariffExpiration(ctx); err == nil && tariff != nil {
			status["tariff_expires_at"] = tariff
		}
	}
	writeJSON(w, http.StatusOK, status)
	return nil
}

// testModeReport tells apart usernames that exist from typos, since a typo would
// otherwise look like the plugin ignoring a tester.
func (p *Plugin) testModeReport(cfg *configuration) map[string]any {
	found, missing := []string{}, []string{}
	for name := range cfg.testers {
		if user, appErr := p.API.GetUserByUsername(name); appErr == nil && user.DeleteAt == 0 {
			found = append(found, name)
		} else {
			missing = append(missing, name)
		}
	}
	sort.Strings(found)
	sort.Strings(missing)
	return map[string]any{"users": found, "unknown": missing, "admins": cfg.TestModeAdmins}
}

func keyErrorText(err error) string {
	if talk.IsStatus(err, http.StatusUnauthorized) {
		return "Толк не принял ключ (HTTP 401): он неверен или истёк."
	}
	return fmt.Sprintf("Не удалось проверить ключ: %v", err)
}

func (p *Plugin) apiAdminRegisterWebhook(w http.ResponseWriter, _ *http.Request, userID string) error {
	if !p.API.HasPermissionTo(userID, model.PermissionManageSystem) {
		return errForbidden
	}
	state, err := p.registerWebhook()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, state)
	return nil
}

func (p *Plugin) apiAdminDeleteWebhook(w http.ResponseWriter, _ *http.Request, userID string) error {
	if !p.API.HasPermissionTo(userID, model.PermissionManageSystem) {
		return errForbidden
	}
	if err := p.removeWebhook(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}

// --- Dialog submissions ---

func dialogFieldError(w http.ResponseWriter, errs map[string]string) {
	writeJSON(w, http.StatusOK, model.SubmitDialogResponse{Errors: errs})
}

func dialogGeneralError(w http.ResponseWriter, err error) bool {
	var known *userError
	if errors.As(err, &known) {
		writeJSON(w, http.StatusOK, model.SubmitDialogResponse{Error: known.message})
		return true
	}
	return false
}

func dialogRequest(r *http.Request, userID string) (*model.SubmitDialogRequest, error) {
	var req model.SubmitDialogRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	// The user comes from the session. The body is only trusted to match it.
	if req.UserId != userID {
		return nil, errForbidden
	}
	return &req, nil
}

func (p *Plugin) dialogStart(w http.ResponseWriter, r *http.Request, userID string) error {
	req, err := dialogRequest(r, userID)
	if err != nil {
		return err
	}
	roomName := strings.TrimSpace(stringField(req.Submission, "room_name"))
	if roomName != "" && !validRoomName(roomName) {
		dialogFieldError(w, map[string]string{"room_name": "От 3 до 64 символов: строчные латинские буквы и цифры."})
		return nil
	}
	rootID, linkOnly := strings.CutPrefix(req.State, linkState)
	m, err := p.startMeeting(userID, createRequest{ChannelID: req.ChannelId, RootID: rootID, Title: stringField(req.Submission, "title"), RoomName: roomName, LinkOnly: linkOnly})
	if err != nil {
		if dialogGeneralError(w, err) {
			return nil
		}
		return err
	}
	p.API.SendEphemeralPost(userID, &model.Post{UserId: p.botUserID, ChannelId: req.ChannelId, RootId: rootID, Message: "Присоединиться к встрече: " + m.URL})
	writeJSON(w, http.StatusOK, model.SubmitDialogResponse{})
	return nil
}

func (p *Plugin) dialogConnect(w http.ResponseWriter, r *http.Request, userID string) error {
	req, err := dialogRequest(r, userID)
	if err != nil {
		return err
	}
	key, err := p.connectPersonalKey(userID, stringField(req.Submission, "token"))
	if err != nil {
		p.API.LogInfo("Personal key rejected", "user_id", userID, "error", err.Error())
		dialogFieldError(w, map[string]string{"token": personalKeyMessage(err)})
		return nil
	}
	p.API.SendEphemeralPost(userID, &model.Post{UserId: p.botUserID, ChannelId: req.ChannelId, Message: personalKeyConnected(key)})
	writeJSON(w, http.StatusOK, model.SubmitDialogResponse{})
	return nil
}

func (p *Plugin) dialogSchedule(w http.ResponseWriter, r *http.Request, userID string) error {
	req, err := dialogRequest(r, userID)
	if err != nil {
		return err
	}
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return appErr
	}
	if req.Type == "refresh" {
		// A field with refresh changed: send the form back, rebuilt for it.
		// Editing is checked here too, so the form is never offered for a
		// meeting the user may not change, though saving would refuse anyway.
		if meetingID, editing := strings.CutPrefix(req.State, "edit:"); editing {
			if _, err := p.controlledMeeting(userID, meetingID, rightSchedule); err != nil {
				if dialogGeneralError(w, err) {
					return nil
				}
				return err
			}
		}
		writeJSON(w, http.StatusOK, model.SubmitDialogResponse{Type: string(model.SubmitDialogResponseTypeForm), Form: refreshedScheduleDialog(req, userLocation(user))})
		return nil
	}
	input, errs := parseScheduleSubmission(req.Submission, time.Now().UTC(), userLocation(user))
	if ids, unknown := p.participantIDs(input.UserIDs); unknown != "" {
		errs["participants"] = "Не найден пользователь: " + unknown
	} else {
		input.UserIDs = ids
	}
	if len(errs) > 0 {
		dialogFieldError(w, errs)
		return nil
	}

	var actionErr error
	if meetingID, editing := strings.CutPrefix(req.State, "edit:"); editing {
		m, err := p.controlledMeeting(userID, meetingID, rightSchedule)
		if err == nil && m.Schedule != nil && m.Schedule.Recurring != (input.Repeat != "") {
			// Talk refuses to turn a series into a single meeting or back.
			dialogFieldError(w, map[string]string{"repeat": "Толк не меняет повторяющуюся встречу на разовую и наоборот. Отмените эту встречу и создайте новую."})
			return nil
		}
		if err == nil {
			err = p.rescheduleMeeting(user, m, input)
		}
		actionErr = err
	} else {
		rootID := strings.TrimPrefix(req.State, "new:")
		channel, chanErr := p.API.GetChannel(req.ChannelId)
		if chanErr != nil {
			return chanErr
		}
		_, actionErr = p.scheduleMeeting(user, channel, rootID, input)
	}
	if actionErr != nil {
		if dialogGeneralError(w, actionErr) {
			return nil
		}
		return actionErr
	}
	writeJSON(w, http.StatusOK, model.SubmitDialogResponse{})
	return nil
}

func (p *Plugin) apiPersonalKeyStatus(w http.ResponseWriter, _ *http.Request, userID string) error {
	writeJSON(w, http.StatusOK, p.personalKeyStatus(userID))
	return nil
}

// apiConnectPersonalKey connects a key typed in the interface. The answer is
// the new status, or a message the form shows under the field.
func (p *Plugin) apiConnectPersonalKey(w http.ResponseWriter, r *http.Request, userID string) error {
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	if _, err := p.connectPersonalKey(userID, req.Token); err != nil {
		p.API.LogInfo("Personal key rejected", "user_id", userID, "error", err.Error())
		return newUserError(http.StatusBadRequest, personalKeyMessage(err))
	}
	p.audit("connectPersonalKey", userID, "")
	p.publishConfigChanged(userID)
	writeJSON(w, http.StatusOK, p.personalKeyStatus(userID))
	return nil
}

func (p *Plugin) apiDisconnectPersonalKey(w http.ResponseWriter, _ *http.Request, userID string) error {
	if err := p.disconnectPersonalKey(userID); err != nil {
		return err
	}
	p.audit("disconnectPersonalKey", userID, "")
	p.publishConfigChanged(userID)
	writeJSON(w, http.StatusOK, p.personalKeyStatus(userID))
	return nil
}
