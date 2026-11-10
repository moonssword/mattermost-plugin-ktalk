package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/mock"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

// recordingWorld is a Talk space with recording r1, owned by Talk user
// k-anna and viewed by k-boris, whose grant to r2 was revoked. Mattermost
// users map to Talk keys through the plugin's cache. "admin" is a system
// admin and a member of every channel, which must not matter.
func recordingWorld(t *testing.T) (*Plugin, map[string]*model.User) {
	t.Helper()
	grants := map[string]string{
		"r1": `{"userAccesses":[{"user":{"key":"k-anna"},"roleId":"owner"},{"user":{"key":"k-boris"},"roleId":"viewer"}],"linkAccess":{"scope":"domain"}}`,
		"r2": `{"userAccesses":[{"user":{"key":"k-anna"},"roleId":"owner"},{"user":{"key":"k-boris"},"roleId":"none"}]}`,
	}
	talkAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
		switch {
		case len(key) == 3 && key[0] == "Recordings" && key[2] == "access" && grants[key[1]] != "":
			_, _ = w.Write([]byte(grants[key[1]]))
		case len(key) == 3 && key[0] == "Domain" && key[1] == "recordings" && grants[key[2]] != "":
			_, _ = w.Write([]byte(`{"key":"` + key[2] + `","title":"Встреча"}`))
		default:
			http.Error(w, "unknown", http.StatusNotFound)
		}
	}))
	t.Cleanup(talkAPI.Close)

	users := map[string]*model.User{
		"anna":       {Id: "anna", Email: "anna@example.com", EmailVerified: true},
		"boris":      {Id: "boris", Email: "boris@example.com", EmailVerified: true},
		"admin":      {Id: "admin", Roles: model.SystemAdminRoleId + " " + model.SystemUserRoleId, Email: "admin@example.com", EmailVerified: true},
		"unverified": {Id: "unverified", Email: "anna@example.com"},
	}
	kv := map[string][]byte{}
	for id, key := range map[string]string{"anna": "k-anna", "boris": "k-boris", "admin": "k-admin"} {
		kv[keyUserMap+id], _ = json.Marshal(talkIdentity{Email: users[id].Email, Key: key})
	}
	api := &plugintest.API{}
	api.On("KVGet", mock.Anything).Return(func(key string) []byte { return kv[key] }, nil)
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(
		func(key string, value []byte, _ model.PluginKVSetOptions) bool { kv[key] = value; return true }, nil)
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("HasPermissionTo", mock.Anything, mock.Anything).Return(true)
	api.On("HasPermissionToChannel", mock.Anything, mock.Anything, mock.Anything).Return(true)
	api.On("GetChannelMember", mock.Anything, mock.Anything).Return(&model.ChannelMember{}, nil)
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	p := &Plugin{config: &configuration{SpaceURL: talkAPI.URL, APIKey: "key", RequireVerifiedEmail: true}}
	p.SetAPI(api)
	return p, users
}

func TestRecordingsFollowTheGrantsInTalk(t *testing.T) {
	p, users := recordingWorld(t)
	cases := []struct {
		user, recording string
		allowed         bool
	}{
		{"anna", "r1", true},        // owner
		{"boris", "r1", true},       // viewer
		{"boris", "r2", false},      // grant revoked in Talk
		{"admin", "r1", false},      // system admin with read access to every channel
		{"unverified", "r1", false}, // an address anyone could type
		{"anna", "missing", false},
	}
	for _, c := range cases {
		_, err := p.recordingForUser(users[c.user], c.recording)
		if (err == nil) != c.allowed {
			t.Errorf("%s reading %s: err %v, want allowed=%v", c.user, c.recording, err, c.allowed)
		}
	}
}

func TestRecordingListKeepsOnlyGrantedRecordings(t *testing.T) {
	p, _ := recordingWorld(t)
	client := p.client()
	recs := []talk.Recording{{Key: "r1"}, {Key: "r2"}, {Key: "missing"}}
	got := p.accessibleRecordings(context.Background(), client, "k-boris", recs)
	if len(got) != 1 || got[0].Key != "r1" {
		t.Fatalf("boris sees %+v", got)
	}
	got = p.accessibleRecordings(context.Background(), client, "k-anna", recs)
	if len(got) != 2 || got[0].Key != "r1" || got[1].Key != "r2" {
		t.Fatalf("anna sees %+v", got)
	}
	if got := p.accessibleRecordings(context.Background(), client, "k-admin", recs); len(got) != 0 {
		t.Fatalf("admin sees %+v", got)
	}
}

func TestRecordingPeriodIsAtMostAMonth(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	from, to, err := recordingPeriod(url.Values{}, now)
	if err != nil || !to.Equal(now) || to.Sub(from) != 30*24*time.Hour {
		t.Fatalf("default period %v..%v, %v", from, to, err)
	}
	ok := url.Values{"from": {"2026-09-30T21:00:00.000Z"}, "to": {"2026-10-31T21:00:00.000Z"}}
	if _, _, err := recordingPeriod(ok, now); err != nil {
		t.Fatalf("31 days refused: %v", err)
	}
	for name, query := range map[string]url.Values{
		"too long": {"from": {"2026-08-01T00:00:00Z"}, "to": {"2026-10-01T00:00:00Z"}},
		"reversed": {"from": {"2026-10-02T00:00:00Z"}, "to": {"2026-10-01T00:00:00Z"}},
		"garbage":  {"from": {"yesterday"}},
	} {
		if _, _, err := recordingPeriod(query, now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// talkRecorder is a Talk space that starts recording r9 and records every
// change of access it is asked for.
func talkRecorder(t *testing.T) (*Plugin, map[string][]byte, *[]string) {
	t.Helper()
	patches := []string{}
	talkAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/Rooms/room1":
			_, _ = w.Write([]byte(`{"roomName":"room1","moderators":[{"key":"k-owner"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/Recordings/start":
			_, _ = w.Write([]byte(`{"recordingKey":"r9"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/Recordings/r9/access":
			_, _ = w.Write([]byte(`{"userAccesses":[{"user":{"key":"k-owner"},"roleId":"owner"},{"user":{"key":"k-guest"},"roleId":"viewer"},{"user":{"key":"k-gone"},"roleId":"none"}]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/Recordings/r9/access":
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			patches = append(patches, string(body))
		default:
			http.Error(w, "unknown", http.StatusNotFound)
		}
	}))
	t.Cleanup(talkAPI.Close)
	kv := map[string][]byte{}
	m := &Meeting{ID: "m1", ChannelID: "c1", PostID: "p1", CreatorID: "u1", CreatorKey: "k-owner", RoomName: "room1", Status: statusLive}
	kv[keyMeeting+"m1"], _ = json.Marshal(m)
	// u1 moderates the room in Talk, which is what lets them record.
	kv[keyUserMap+"u1"], _ = json.Marshal(talkIdentity{Email: "u1@example.com", Key: "k-owner"})
	api := &plugintest.API{}
	api.On("KVGet", mock.Anything).Return(func(key string) []byte { return kv[key] }, nil)
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(
		func(key string, value []byte, _ model.PluginKVSetOptions) bool { kv[key] = value; return true }, nil)
	api.On("KVDelete", mock.Anything).Return(nil)
	api.On("GetUser", "u1").Return(&model.User{Id: "u1", Email: "u1@example.com", EmailVerified: true}, nil)
	api.On("HasPermissionTo", mock.Anything, mock.Anything).Return(false)
	api.On("GetPost", "p1").Return(&model.Post{Id: "p1"}, nil)
	api.On("UpdatePost", mock.Anything).Return(&model.Post{}, nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything)
	api.On("LogAuditRec", mock.Anything)
	for _, level := range []string{"LogDebug", "LogInfo", "LogWarn", "LogError"} {
		api.On(level, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On(level, mock.Anything, mock.Anything, mock.Anything).Maybe()
	}
	p := &Plugin{config: &configuration{SpaceURL: talkAPI.URL, APIKey: "key"}}
	p.SetAPI(api)
	return p, kv, &patches
}

func TestTheChosenAccessWaitsForTheFinishedRecording(t *testing.T) {
	for access, scope := range map[string]string{
		accessParticipants: `"scope":"none"`,
		accessSelf:         `"scope":"none"`,
		accessDomain:       `"scope":"domain"`,
		accessGlobal:       `"scope":"global"`,
		"":                 `"scope":"none"`, // the default when nothing is chosen
	} {
		p, kv, patches := talkRecorder(t)
		if err := p.startRecording("u1", "m1", access); err != nil {
			t.Fatalf("%q: %v", access, err)
		}
		if len(*patches) != 0 {
			t.Errorf("%q: access changed while recording: %v", access, *patches)
		}
		var m Meeting
		_ = json.Unmarshal(kv[keyMeeting+"m1"], &m)
		want := access
		if want == "" {
			want = accessParticipants
		}
		choice := m.RecordingChoices["r9"]
		if choice.Access != want || choice.Owner != "k-owner" || choice.UserID != "u1" || !m.Recording {
			t.Errorf("%q: stored %+v", access, choice)
		}
		p.applyRecordingChoice(p.client(), "r9", choice)
		if len(*patches) != 1 || !strings.Contains((*patches)[0], scope) {
			t.Errorf("%q: patches %v", access, *patches)
		}
	}
	p, _, _ := talkRecorder(t)
	if err := p.startRecording("u1", "m1", "everyone"); err == nil {
		t.Error("an unknown access was accepted")
	}
}

func TestOnlyMeRevokesEveryoneButTheOwner(t *testing.T) {
	p, _, patches := talkRecorder(t)
	if err := p.restrictToOwner(p.client(), "r9", "k-owner"); err != nil {
		t.Fatal(err)
	}
	if len(*patches) != 1 {
		t.Fatalf("patches %v", *patches)
	}
	got := (*patches)[0]
	if !strings.Contains(got, `{"roleId":"none","userKey":"k-guest"}`) || strings.Contains(got, "k-owner") || strings.Contains(got, "k-gone") || !strings.Contains(got, `"scope":"none"`) {
		t.Errorf("patch %s", got)
	}
}

func TestAccessQuestionAnswersPerRecording(t *testing.T) {
	p, users := recordingWorld(t)
	api := p.API.(*plugintest.API)
	for id, user := range users {
		api.On("GetUser", id).Return(user, nil)
	}
	ask := func(userID, body string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/recordings/access", strings.NewReader(body))
		req.Header.Set("Mattermost-User-Id", userID)
		rec := httptest.NewRecorder()
		p.authenticated(p.apiRecordingsAccess).ServeHTTP(rec, req)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	if code, got := ask("boris", `{"keys":["r1","r2","missing"]}`); code != http.StatusOK || got != `{"access":{"missing":false,"r1":true,"r2":false}}` {
		t.Errorf("boris: %d %s", code, got)
	}
	if _, got := ask("admin", `{"keys":["r1"]}`); got != `{"access":{"r1":false}}` {
		t.Errorf("admin: %s", got)
	}
	if _, got := ask("unverified", `{"keys":["r1"]}`); got != `{"access":{"r1":false}}` {
		t.Errorf("unverified: %s", got)
	}
	many := `{"keys":[` + strings.TrimSuffix(strings.Repeat(`"k",`, maxAccessKeys+1), ",") + `]}`
	if code, _ := ask("anna", many); code != http.StatusBadRequest {
		t.Errorf("too many keys: %d", code)
	}
}

func TestAddressesFromTheIdentityProviderCountAsVerified(t *testing.T) {
	p := &Plugin{config: &configuration{RequireVerifiedEmail: true}}
	cases := []struct {
		name string
		user *model.User
		want string
	}{
		{"password, verified", &model.User{Email: "a@example.com", EmailVerified: true}, "a@example.com"},
		{"password, unverified", &model.User{Email: "a@example.com"}, ""},
		{"SAML, flag unset", &model.User{Email: "B@example.com", AuthService: model.UserAuthServiceSaml}, "b@example.com"},
		{"LDAP, flag unset", &model.User{Email: "c@example.com", AuthService: model.UserAuthServiceLdap}, "c@example.com"},
		{"explicit email service", &model.User{Email: "d@example.com", AuthService: model.UserAuthServiceEmail}, ""},
		{"deactivated SAML", &model.User{Email: "e@example.com", AuthService: model.UserAuthServiceSaml, DeleteAt: 1}, ""},
	}
	for _, c := range cases {
		if got := p.talkEmail(c.user); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
