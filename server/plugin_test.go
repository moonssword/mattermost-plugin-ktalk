package main

import (
	"bytes"
	"encoding/gob"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/mock"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

func TestGeneratedRoomNamesAreAccepted(t *testing.T) {
	for _, scheme := range []string{schemeWords, schemeUUID, schemeContext, schemeAsk, "unknown"} {
		for range 200 {
			name := generateRoomName(scheme, "mm", "My-Team_1", "town-square_very-long-channel-name-that-goes-on-and-on-and-on-forever")
			if !validRoomName(name) {
				t.Fatalf("scheme %s produced invalid %q", scheme, name)
			}
		}
	}
}

func TestPersonalRoomIsStableAndValid(t *testing.T) {
	a, b := personalRoomName("mm", "user1"), personalRoomName("mm", "user1")
	if a != b || !validRoomName(a) || a == personalRoomName("mm", "user2") {
		t.Fatalf("a=%q b=%q", a, b)
	}
}

func TestValidRoomName(t *testing.T) {
	for name, want := range map[string]bool{"abc123": true, "ab": false, "Abc": false, "a-b-c": false, "a_b_c": false, "": false} {
		if got := validRoomName(name); got != want {
			t.Errorf("%q: got %v", name, got)
		}
	}
}

func TestConfigurationValidation(t *testing.T) {
	cases := map[string]bool{
		"":                             true,
		"https://x.ktalk.ru":           true,
		"https://x.ktalk.ru/":          true,
		"https://x.ktalk.ru/some/path": false,
		"x.ktalk.ru":                   false,
		"ftp://x.ktalk.ru":             false,
		"http://x.ktalk.ru":            false, // the key would travel in clear
		"http://localhost:8080":        true,
		"http://127.0.0.1":             true,
	}
	for raw, ok := range cases {
		err := (&configuration{SpaceURL: raw}).validate()
		if (err == nil) != ok {
			t.Errorf("%q: err=%v", raw, err)
		}
	}
}

func TestLinksFollowConfiguredSpace(t *testing.T) {
	cfg := &configuration{SpaceURL: "https://prod.example.ru/"}
	if got := cfg.roomURL("abc"); got != "https://prod.example.ru/abc" {
		t.Error(got)
	}
	if got := cfg.recordingURL("k1"); got != "https://prod.example.ru/recordings/k1" {
		t.Error(got)
	}
	if got := cfg.artifactsURL("c1"); got != "https://prod.example.ru/content/artifacts?cid=c1" {
		t.Error(got)
	}
}

func TestRoomStateLifecycle(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	m := &Meeting{Status: statusWaiting, CreatedAt: now}

	if applyRoomState(m, &talk.Room{}, now) || m.Status != statusWaiting {
		t.Fatalf("empty new room changed state: %s", m.Status)
	}
	online := &talk.Room{UsersOnline: 2, OnlineUsers: []talk.OnlineUser{{IsAnonymous: true, AnonymousName: "Гость"}}}
	applyRoomState(m, online, now.Add(time.Minute))
	if m.Status != statusLive || m.StartedAt == nil || m.UsersOnline != 2 || m.Online[0] != "Гость" {
		t.Fatalf("not live: %+v", m)
	}

	// One empty poll may be a reconnect.
	if applyRoomState(m, &talk.Room{}, now.Add(2*time.Minute)) || m.Status != statusLive {
		t.Fatal("finished after a single empty poll")
	}
	applyRoomState(m, online, now.Add(3*time.Minute))
	if m.EmptyPolls != 0 {
		t.Fatal("empty polls not reset on return")
	}
	started := *m.StartedAt
	if applyRoomState(m, &talk.Room{}, now.Add(4*time.Minute)) {
		t.Fatal("finished too early")
	}
	if !applyRoomState(m, &talk.Room{}, now.Add(5*time.Minute)) || m.Status != statusEnded || m.EndedAt == nil {
		t.Fatalf("not finished: %+v", m)
	}
	if !m.StartedAt.Equal(started) {
		t.Fatal("start time moved")
	}
}

func TestRingingTimesOut(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	m := &Meeting{Status: statusRinging, RingingUntil: now.Add(time.Minute), Callees: []CallCallee{{Status: "calling"}, {Status: "rejected"}}}
	applyRoomState(m, &talk.Room{}, now)
	if m.Status != statusRinging {
		t.Fatal("stopped ringing early")
	}
	applyRoomState(m, &talk.Room{}, now.Add(2*time.Minute))
	if m.Status != statusWaiting || m.Callees[0].Status != "no_answer" || m.Callees[1].Status != "rejected" {
		t.Fatalf("after timeout: %+v", m)
	}
}

func TestMissedCallCloses(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	m := &Meeting{Kind: kindCall, Status: statusRinging, CreatedAt: now, RingingUntil: now.Add(ringingTimeout), Callees: []CallCallee{{Status: calleeCalling}}}
	applyRoomState(m, &talk.Room{}, now.Add(ringingTimeout+time.Second))
	if m.Status != statusWaiting {
		t.Fatalf("after ringing: %s", m.Status)
	}
	applyRoomState(m, &talk.Room{}, now.Add(ringingTimeout+missedCallGrace-time.Second))
	if m.Status != statusWaiting {
		t.Fatal("closed before the grace period")
	}
	applyRoomState(m, &talk.Room{}, now.Add(ringingTimeout+missedCallGrace+time.Second))
	if m.Status != statusEnded || !m.Expired {
		t.Fatalf("missed call still open: %+v", m)
	}
}

func TestAcceptedCallWaitsLonger(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	m := &Meeting{Kind: kindCall, Status: statusWaiting, CreatedAt: now, RingingUntil: now.Add(10 * time.Second), Callees: []CallCallee{{Status: calleeAccepted}}}
	applyRoomState(m, &talk.Room{}, now.Add(5*time.Minute))
	if m.Status != statusWaiting {
		t.Fatal("accepted call closed too early")
	}
	applyRoomState(m, &talk.Room{}, now.Add(acceptedCallGrace+time.Minute))
	if m.Status != statusEnded {
		t.Fatal("accepted call never closed")
	}
	// A meeting that is not a call keeps the long wait.
	other := &Meeting{Status: statusWaiting, CreatedAt: now}
	applyRoomState(other, &talk.Room{}, now.Add(time.Hour))
	if other.Status != statusWaiting {
		t.Fatal("ordinary meeting closed like a call")
	}
}

func TestUnusedMeetingExpires(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	m := &Meeting{Status: statusWaiting, CreatedAt: now}
	applyRoomState(m, &talk.Room{}, now.Add(unusedMeetingTTL+time.Minute))
	if m.Status != statusEnded || !m.Expired {
		t.Fatalf("not expired: %+v", m)
	}
	if !artifactsSettled(m, now.Add(unusedMeetingTTL+time.Minute)) {
		t.Fatal("expired meeting keeps polling")
	}
}

func TestScheduledMeetingWaitsAndGoesLive(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	m := &Meeting{Status: statusScheduled, CreatedAt: now.Add(-48 * time.Hour), Schedule: &Schedule{Start: now.Add(time.Hour), End: now.Add(2 * time.Hour)}}
	applyRoomState(m, &talk.Room{}, now)
	if m.Status != statusScheduled {
		t.Fatal("scheduled meeting expired by creation age")
	}
	applyRoomState(m, &talk.Room{UsersOnline: 1}, now.Add(time.Hour))
	if m.Status != statusLive {
		t.Fatal("did not go live")
	}
}

func TestArtifactsSettled(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	ended := now
	m := &Meeting{Status: statusEnded, EndedAt: &ended}
	if artifactsSettled(m, now.Add(time.Hour)) {
		t.Fatal("recording may still be processing")
	}
	if !artifactsSettled(m, now.Add(noRecordingWindow+time.Minute)) {
		t.Fatal("no recordings: should stop")
	}
	m.Recordings = []MeetingRecording{{Key: "a"}}
	if artifactsSettled(m, now.Add(noRecordingWindow+time.Minute)) {
		t.Fatal("waits for the summary")
	}
	m.Recordings[0].SummaryDone = true
	if !artifactsSettled(m, now.Add(time.Hour)) {
		t.Fatal("summary done: should stop")
	}
	m.Recordings[0].SummaryDone = false
	if !artifactsSettled(m, now.Add(artifactWindow+time.Minute)) {
		t.Fatal("must give up eventually")
	}
}

func TestRoomPolicy(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	cfg := &configuration{PublicLobby: true, PublicGuestHours: 4, MuteAboveMembers: 15}
	if p := computeRoomPolicy("D", 2, cfg, now); p.Lobby || p.GuestsUntil != nil || p.Muted {
		t.Fatalf("direct: %+v", p)
	}
	if p := computeRoomPolicy("P", 100, cfg, now); !p.Lobby || p.GuestsUntil != nil || p.Muted {
		t.Fatalf("private channels must stay closed to guests: %+v", p)
	}
	p := computeRoomPolicy("O", 100, cfg, now)
	if !p.Lobby || !p.Muted || p.GuestsUntil == nil || !p.GuestsUntil.Equal(now.Add(4*time.Hour)) {
		t.Fatalf("public: %+v", p)
	}
	if p := computeRoomPolicy("O", 3, &configuration{MuteAboveMembers: 15}, now); p.Muted || p.GuestsUntil != nil {
		t.Fatalf("small public: %+v", p)
	}
}

func TestViewHidesServerFields(t *testing.T) {
	m := &Meeting{ID: "1", CreatorKey: "secret-key", ConferenceKey: "conf", Moderators: []string{"mod"},
		Callees:    []CallCallee{{Name: "A", TalkKey: "talk-key", Status: "calling"}},
		Recordings: []MeetingRecording{{Key: "r", Posted: true}},
		Schedule:   &Schedule{Email: "boss@example.ru", ExternalEmails: []string{"x@y.z"}, UserKeys: []string{"k"}}}
	data := mustJSON(t, m.view())
	for _, hidden := range []string{"secret-key", "talk-key", "boss@example.ru", "x@y.z", "\"mod\"", "conference_key", "posted"} {
		if strings.Contains(data, hidden) {
			t.Errorf("view leaks %s: %s", hidden, data)
		}
	}
}

func TestSafeInlineNeutralizesMentions(t *testing.T) {
	if got := safeInline("x](https://evil.example) [y"); got != `x\](https://evil.example) \[y` {
		t.Errorf("link marks survive: %q", got)
	}
	if strings.Contains(safeInline("hi @channel"), "@channel") {
		t.Fatal("mention survives")
	}
}

func TestClip(t *testing.T) {
	if got := clip("  абвгд  ", 10); got != "абвгд" {
		t.Error(got)
	}
	if got := clip("абвгдежз", 4); got != "абв…" {
		t.Error(got)
	}
}

func TestValidPin(t *testing.T) {
	for pin, want := range map[string]bool{"1234": true, "123456": true, "123": false, "1234567": false, "12a4": false} {
		if validPin(pin) != want {
			t.Errorf("%q", pin)
		}
	}
}

func TestTimezoneLabel(t *testing.T) {
	tz := func(name string) time.Time {
		location, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		return time.Date(2026, 11, 7, 12, 0, 0, 0, location)
	}
	for name, want := range map[string]string{"Europe/Moscow": "GMT+3", "UTC": "GMT+0", "Asia/Kolkata": "GMT+5:30", "America/New_York": "GMT-5"} {
		if got := timezoneLabel(tz(name)); got != want {
			t.Errorf("%s: %s", name, got)
		}
	}
}

func TestScheduleSubmission(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	good := map[string]any{"title": " Планёрка ", "start": "2026-11-08T09:00:00Z", "duration": "30", "repeat": "weekly", "repeat_end": "count", "repeat_count": "4",
		"participants": []any{"u1", "u2"}, "external": "a@b.ru, c@d.ru", "pin": "1234", "lobby": true, "guests": "true"}
	in, errs := parseScheduleSubmission(good, now, time.UTC)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if in.Title != "Планёрка" || in.DurationMin != 30 || in.RepeatCount != 4 || len(in.UserIDs) != 2 || len(in.ExternalEmails) != 2 || !in.Lobby || !in.Guests {
		t.Fatalf("%+v", in)
	}
	bad := map[string]any{"title": "", "start": "2020-01-01T00:00:00Z", "duration": "x", "repeat": "daily", "repeat_end": "count", "repeat_count": "1", "external": "not-an-email", "pin": "12"}
	_, errs = parseScheduleSubmission(bad, now, time.UTC)
	// The repeat is checked against a valid start only; see repeat_test.go.
	for _, field := range []string{"title", "start", "duration", "external", "pin"} {
		if errs[field] == "" {
			t.Errorf("no error for %s", field)
		}
	}
}

func TestSplitCommand(t *testing.T) {
	for line, want := range map[string][2]string{
		"/talk": {"", ""},
		"/talk start Планёрка команды": {"start", "Планёрка команды"},
		"/talk PIN 1234": {"pin", "1234"},
	} {
		sub, rest := splitCommand(line)
		if sub != want[0] || rest != want[1] {
			t.Errorf("%q: %q %q", line, sub, rest)
		}
	}
}

func TestLatestConference(t *testing.T) {
	older := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	got := latestConference([]talk.ConferenceInfo{{Key: "old", StartTime: older}, {Key: "new", StartTime: older.Add(time.Hour)}})
	if got != "new" || latestConference(nil) != "" {
		t.Fatal(got)
	}
}

func TestSecretIsGeneratedOnceAndKept(t *testing.T) {
	api := &plugintest.API{}
	stored := map[string][]byte{}
	api.On("KVGet", mock.Anything).Return(func(key string) []byte { return stored[key] }, nil)
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(
		func(key string, value []byte, options model.PluginKVSetOptions) bool {
			if options.Atomic && stored[key] != nil {
				return false // Another node got there first.
			}
			stored[key] = value
			return true
		}, nil)
	p := &Plugin{}
	p.SetAPI(api)

	first, err := p.secret(secretWebhook)
	if err != nil || first == "" {
		t.Fatalf("first=%q err=%v", first, err)
	}
	second, err := p.secret(secretWebhook)
	if err != nil || second != first {
		t.Fatalf("secret changed: %q != %q (err=%v)", second, first, err)
	}
	other, err := p.secret(secretEncryption)
	if err != nil || other == first {
		t.Fatalf("secrets must differ: %q (err=%v)", other, err)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	api := &plugintest.API{}
	stored := map[string][]byte{}
	api.On("KVGet", mock.Anything).Return(func(key string) []byte { return stored[key] }, nil)
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(
		func(key string, value []byte, _ model.PluginKVSetOptions) bool { stored[key] = value; return true }, nil)
	p := &Plugin{}
	p.SetAPI(api)

	encrypted, err := p.encrypt("personal-token")
	if err != nil || strings.Contains(encrypted, "personal-token") {
		t.Fatalf("encrypted=%q err=%v", encrypted, err)
	}
	plain, err := p.decrypt(encrypted)
	if err != nil || plain != "personal-token" {
		t.Fatalf("plain=%q err=%v", plain, err)
	}
}

func TestWebhookRejectsWrongSecret(t *testing.T) {
	api := &plugintest.API{}
	stored := map[string][]byte{}
	api.On("KVGet", mock.Anything).Return(func(key string) []byte { return stored[key] }, nil)
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(
		func(key string, value []byte, _ model.PluginKVSetOptions) bool { stored[key] = value; return true }, nil)
	p := &Plugin{config: &configuration{EnableWebhooks: true}}
	p.SetAPI(api)
	secret, err := p.secret(secretWebhook)
	if err != nil {
		t.Fatal(err)
	}

	call := func(header string, enabled bool) int {
		p.config.EnableWebhooks = enabled
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
		if header != "" {
			req.Header.Set(webhookSecretHeader, header)
		}
		rec := httptest.NewRecorder()
		p.handleWebhook(rec, req)
		return rec.Code
	}
	if got := call("", true); got != http.StatusForbidden {
		t.Errorf("no header: %d", got)
	}
	if got := call("wrong", true); got != http.StatusForbidden {
		t.Errorf("wrong secret: %d", got)
	}
	if got := call(secret, false); got != http.StatusForbidden {
		t.Errorf("webhooks disabled: %d", got)
	}
	if got := call(secret, true); got != http.StatusOK {
		t.Errorf("valid request: %d", got)
	}
}

// Post props travel through gob between the plugin and the server. An
// unregistered type in them crashes the plugin process.
func TestViewSurvivesPluginRPC(t *testing.T) {
	gob.Register(map[string]any{})
	gob.Register([]any{})
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	m := &Meeting{ID: "1", CreatedAt: now, StartedAt: &now, GuestsUntil: &now, SIP: &talk.SIPSettings{SIPNumber: "1"},
		Online: []string{"a"}, Callees: []CallCallee{{Name: "A"}}, Recordings: []MeetingRecording{{Key: "r"}},
		Results:  &MeetingResults{Attended: []string{"a"}},
		Schedule: &Schedule{Start: now, End: now, Attendees: []MeetingAttendee{{Name: "A", Status: "accepted"}}}}
	for name, view := range map[string]map[string]any{"full": m.view(), "empty": (&Meeting{}).view()} {
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(view); err != nil {
			t.Fatalf("%s view does not encode: %v", name, err)
		}
	}
	if m.view()["online"] == nil || (&Meeting{}).view()["started_at"] != nil {
		t.Fatal("view lost a value or kept a null")
	}
}

func TestIsWebURL(t *testing.T) {
	for raw, want := range map[string]bool{"https://a.ru/x": true, "http://a.ru": true, "javascript:alert(1)": false, "JAVASCRIPT:alert(1)": false, "data:text/html,x": false, "": false} {
		if isWebURL(raw) != want {
			t.Errorf("%q", raw)
		}
	}
}

// Mattermost refuses post types longer than this. A longer one fails at
// runtime with a generic "cannot save" error.
func TestPostTypesFitLimit(t *testing.T) {
	for _, postType := range []string{postTypeCard, postTypeRec} {
		if len(postType) > 26 || !strings.HasPrefix(postType, "custom_") {
			t.Errorf("%q does not fit", postType)
		}
	}
}

// Servers before 12.0 count these limits in bytes, so Cyrillic labels are short.
func TestDialogsFitServerLimits(t *testing.T) {
	const limit = 24
	start := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	edit := &Meeting{ID: "1", Title: "x", Schedule: &Schedule{Start: start, End: start.Add(time.Hour)}}
	// The repeat fields of "Другое…" appear only once chosen, so they are
	// laid out too, with both ways to end a series.
	custom := func(end string) model.Dialog {
		values := map[string]string{"start": start.Format(time.RFC3339), "duration": "60", "repeat": repeatCustom, "repeat_end": end}
		return scheduleForm(values, "new:", time.UTC, start)
	}
	for _, dialog := range []model.Dialog{scheduleDialog(nil, "", "", "UTC"), scheduleDialog(edit, "Иван Петров (@ivan.petrov)", "", "UTC"), custom(repeatEndDate), custom(repeatEndCount)} {
		if len(dialog.Title) > limit {
			t.Errorf("title %q is %d bytes", dialog.Title, len(dialog.Title))
		}
		for _, element := range dialog.Elements {
			if len(element.DisplayName) > limit {
				t.Errorf("label %q is %d bytes", element.DisplayName, len(element.DisplayName))
			}
		}
		if err := dialog.IsValid(); err != nil {
			t.Errorf("dialog %q: %v", dialog.Title, err)
		}
	}
}

func TestPlainTitle(t *testing.T) {
	for in, want := range map[string]string{
		"**Жирная** тема\nвторая строка":                                      "Жирная тема",
		"Встреча **Звонок: admin** в Толке. [Присоединиться](https://x.ru/a)": "Встреча Звонок: admin в Толке. Присоединиться",
		"# Заголовок":                  "Заголовок",
		"[ссылка](https://x.ru) текст": "ссылка текст",
		"`код` тут":                    "код тут",
	} {
		if got := plainTitle(in); got != want {
			t.Errorf("%q: got %q", in, got)
		}
	}
}

func TestRecordingReady(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	recent, old := now.Add(-time.Minute), now.Add(-2*recordingSettleTime)
	inProgress, finished := &talk.Recording{Duration: 0}, &talk.Recording{Duration: 67}

	if recordingReady(&Meeting{Status: statusLive, Recording: true}, finished, now) {
		t.Error("announced while still recording")
	}
	if recordingReady(&Meeting{Status: statusLive}, inProgress, now) {
		t.Error("announced an in-progress recording")
	}
	if !recordingReady(&Meeting{Status: statusLive}, finished, now) {
		t.Error("finished recording during a live meeting must be announced")
	}
	if recordingReady(&Meeting{Status: statusEnded, EndedAt: &recent}, inProgress, now) {
		t.Error("announced before processing could finish")
	}
	if !recordingReady(&Meeting{Status: statusEnded, EndedAt: &old}, inProgress, now) {
		t.Error("gave up on a recording with no duration")
	}
}

func TestSummaryFinalStates(t *testing.T) {
	for status, want := range map[string]bool{"success": true, "failed": true, "inProgress": false, "notFound": false, "notAvailable": false, "recreateInProgress": false, "": false} {
		if summaryIsFinal(status) != want {
			t.Errorf("%q", status)
		}
	}
}

func TestNeedsResults(t *testing.T) {
	if needsResults(&Meeting{Status: statusLive}) || needsResults(&Meeting{Status: statusEnded, Expired: true}) || needsResults(&Meeting{Status: statusEnded, Results: &MeetingResults{}}) {
		t.Error("asked for results that are not due")
	}
	if !needsResults(&Meeting{Status: statusEnded}) {
		t.Error("finished conference should look for results")
	}
}

func TestTestModeOnlyListedUsers(t *testing.T) {
	cfg := &configuration{TestMode: true, TestModeUsers: " @Anna, boris;\nvera  "}
	cfg.applyDefaults()
	admin := &model.User{Username: "boss", Roles: "system_user system_admin"}
	for name, want := range map[string]bool{"anna": true, "ANNA": true, "boris": true, "vera": true, "gleb": false} {
		if got := cfg.allowsUser(&model.User{Username: name, Roles: "system_user"}); got != want {
			t.Errorf("%q: got %v", name, got)
		}
	}
	if cfg.allowsUser(admin) {
		t.Error("system administrators must not get access by default")
	}
	if !cfg.allowsUser(&model.User{Username: "anna", Roles: "system_user system_admin"}) {
		t.Error("a listed administrator is a tester like anyone else")
	}
	cfg.TestModeAdmins = true
	if !cfg.allowsUser(admin) {
		t.Error("the admin setting opens the plugin to every system administrator")
	}
	empty := &configuration{TestMode: true}
	empty.applyDefaults()
	if empty.allowsUser(admin) || empty.allowsUser(&model.User{Username: "anna", Roles: "system_user"}) {
		t.Error("with no testers and no admin access nobody may use the plugin")
	}
	off := &configuration{TestModeUsers: "anna"}
	off.applyDefaults()
	if !off.allowsUser(&model.User{Username: "gleb", Roles: "system_user"}) {
		t.Error("the list must not apply when test mode is off")
	}
	if cfg.allowsUser(nil) {
		t.Error("an unknown user must be refused")
	}
}

func TestTestModeClosesRoutesAndCommands(t *testing.T) {
	api := &plugintest.API{}
	api.On("GetUser", "outsider").Return(&model.User{Id: "outsider", Username: "gleb", Roles: "system_user"}, nil)
	api.On("GetUser", "tester").Return(&model.User{Id: "tester", Username: "anna", Roles: "system_user"}, nil)
	cfg := &configuration{TestMode: true, TestModeUsers: "anna"}
	cfg.applyDefaults()
	p := &Plugin{config: cfg}
	p.SetAPI(api)

	reached := false
	handler := p.authenticated(p.testModeGate(func(w http.ResponseWriter, _ *http.Request, _ string) error {
		reached = true
		w.WriteHeader(http.StatusOK)
		return nil
	}))
	call := func(userID string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/upcoming", nil)
		req.Header.Set("Mattermost-User-Id", userID)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := call("outsider"); got != http.StatusForbidden || reached {
		t.Fatalf("outsider: code %d, reached %v", got, reached)
	}
	if got := call("tester"); got != http.StatusOK || !reached {
		t.Fatalf("tester: code %d, reached %v", got, reached)
	}
	response, appErr := p.ExecuteCommand(nil, &model.CommandArgs{UserId: "outsider", Command: "/talk start"})
	if appErr != nil || response.Text != testModeMessage {
		t.Fatalf("command: %+v %v", response, appErr)
	}
}

func TestStyleFallsBackToCurrent(t *testing.T) {
	cfg := &configuration{CardStyle: "nonsense"}
	cfg.applyDefaults()
	if cfg.CardStyle != styleCurrent {
		t.Fatal(cfg.CardStyle)
	}
}

func TestIntFieldAcceptsNumbersAndText(t *testing.T) {
	sub := map[string]any{"a": float64(2), "b": " 12 ", "c": 2.5, "d": "два", "e": nil}
	for key, want := range map[string]struct {
		n  int
		ok bool
	}{"a": {2, true}, "b": {12, true}, "c": {0, false}, "d": {0, false}, "e": {0, false}, "missing": {0, false}} {
		n, ok := intField(sub, key)
		if n != want.n || ok != want.ok {
			t.Errorf("%s: got %d %v", key, n, ok)
		}
	}
}

func TestScheduleRepeatCountFromMattermost11(t *testing.T) {
	sub := map[string]any{"title": "Т", "start": "2030-01-01T10:00:00Z", "duration": "60", "repeat": "daily", "repeat_end": "count", "repeat_count": float64(2)}
	_, errs := parseScheduleSubmission(sub, time.Date(2029, 12, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	if msg, bad := errs["repeat_count"]; bad {
		t.Fatalf("a numeric 2 was rejected: %s", msg)
	}
}

func TestPersonalKeyMessages(t *testing.T) {
	cases := map[string]error{
		"неверный":     &talk.Error{Status: http.StatusUnauthorized},
		"ключу доступ": &talk.Error{Status: http.StatusForbidden},
		"ещё раз":      &talk.Error{Status: http.StatusBadGateway},
		"почту":        newUserError(http.StatusBadRequest, "Подтвердите почту."),
	}
	for want, err := range cases {
		if got := personalKeyMessage(err); !strings.Contains(got, want) {
			t.Errorf("%v: %q", err, got)
		}
	}
}

func TestSameNameIgnoresOrderButNotWords(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Петров Иван", "Иван Петров", true},
		{"Петров Иван ", "иван петров", true},
		{"Анна Смирнова", "Борис Петров", false},
		{"Иван Петров", "Иван", false}, // a shorter name is not the same person
		{"Иван Петров", "Иван Петров Сидоров", false},
		{"", "Кто угодно", false}, // nothing to match means no match
	}
	for _, c := range cases {
		if got := sameName(c.a, c.b); got != c.want {
			t.Errorf("%q vs %q: %v", c.a, c.b, got)
		}
	}
}

func TestScheduleAnswersByUser(t *testing.T) {
	sc := &Schedule{
		Attendees:   []MeetingAttendee{{Name: "Анна", Status: "accepted", Key: "k1"}, {Name: "Гость", Status: "unknown"}, {Name: "Борис", Status: "reject", Key: "k2"}},
		InviteeKeys: map[string]string{"u1": "k1", "u2": "k2", "u3": "k3"},
	}
	got := sc.answers()
	if got["u1"] != "accepted" || got["u2"] != "reject" || len(got) != 2 {
		t.Fatalf("answers %v", got)
	}
}

func TestAnsweredOtherwiseFindsTheUsersOwnAnswer(t *testing.T) {
	item := &talk.CalendarItem{RequiredAttendees: []talk.Attendee{
		{Name: "Анна", Status: "accepted", User: &talk.User{Key: "k1"}},
		{Name: "Борис", Status: "reject", User: &talk.User{Key: "k2"}},
		{Name: "Вера", Status: "unknown", User: &talk.User{Key: "k3"}},
	}}
	if answeredOtherwise(item, "k1") || !answeredOtherwise(item, "k2") || !answeredOtherwise(item, "k3") || answeredOtherwise(item, "missing") {
		t.Fatal("wrong occurrence answers")
	}
}

func TestCollapseSeriesKeepsTheNearestOccurrence(t *testing.T) {
	day := time.Date(2030, 1, 7, 10, 0, 0, 0, time.UTC)
	events := []upcomingEvent{
		{ID: "d2", Subject: "Планёрка", Start: day.Add(24 * time.Hour), Recurring: true, series: "room:a"},
		{ID: "one", Subject: "Разовая", Start: day.Add(2 * time.Hour)},
		{ID: "d1", Subject: "Планёрка", Start: day, Recurring: true, series: "room:a"},
		{ID: "d3", Subject: "Планёрка", Start: day.Add(48 * time.Hour), Recurring: true, series: "room:a"},
		{ID: "w1", Subject: "Ретро", Start: day.Add(3 * time.Hour), Recurring: true, series: "room:b", Repeat: "weekly"},
		{ID: "x1", Subject: "Синк", Start: day.Add(4 * time.Hour), Recurring: true, series: "room:c"},
		{ID: "x2", Subject: "Синк", Start: day.Add(4*time.Hour + 72*time.Hour), Recurring: true, series: "room:c"},
	}
	got := collapseSeries(events)
	ids := []string{}
	for _, event := range got {
		ids = append(ids, event.ID+":"+event.Repeat)
	}
	want := "d1:daily one: w1:weekly x1:"
	if strings.Join(ids, " ") != want {
		t.Fatalf("got %q, want %q", strings.Join(ids, " "), want)
	}
}

func TestParticipantValueReadsAndRoundTrips(t *testing.T) {
	ivan := &model.User{Username: "ivan.petrov", FirstName: "Иван", LastName: "Петров, отдел"}
	value := participantValue(ivan)
	if value != "Иван Петров отдел (@ivan.petrov)" {
		t.Errorf("value %q", value)
	}
	if strings.Contains(value, ",") {
		t.Error("a comma would split the value")
	}
	if got := participantUsername(value); got != "ivan.petrov" {
		t.Errorf("username %q", got)
	}
	if got := participantValue(&model.User{Username: "bob"}); got != "@bob" || participantUsername(got) != "bob" {
		t.Errorf("bare username %q", got)
	}
}
