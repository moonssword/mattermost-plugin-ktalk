package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/mock"
)

func TestDiffPresence(t *testing.T) {
	joined, left := diffPresence([]string{"a", "b"}, []string{"b", "c"})
	if !reflect.DeepEqual(joined, []string{"c"}) || !reflect.DeepEqual(left, []string{"a"}) {
		t.Fatalf("joined=%v left=%v", joined, left)
	}
	joined, left = diffPresence(nil, nil)
	if joined != nil || left != nil {
		t.Fatal("nothing changed")
	}
}

// statusWorld is a fake server holding one user, their preferences and KV.
type statusWorld struct {
	user   *model.User
	kv     map[string][]byte
	prefOn string
	recent string
}

func newStatusWorld(t *testing.T, current *model.CustomStatus) (*Plugin, *statusWorld) {
	t.Helper()
	w := &statusWorld{user: &model.User{Id: "u1", Locale: "ru"}, kv: map[string][]byte{}, prefOn: "true"}
	if current != nil {
		_ = w.user.SetCustomStatus(current)
	}
	api := &plugintest.API{}
	api.On("GetUser", "u1").Return(func(string) *model.User { return w.user }, nil)
	api.On("GetPreferenceForUser", "u1", prefCategory, prefMeetingStatus).Return(func(string, string, string) model.Preference {
		return model.Preference{Value: w.prefOn}
	}, nil)
	api.On("GetPreferenceForUser", "u1", prefCustomStatusCategory, prefRecentCustomStatuses).Return(func(string, string, string) model.Preference {
		return model.Preference{UserId: "u1", Category: prefCustomStatusCategory, Name: prefRecentCustomStatuses, Value: w.recent}
	}, nil)
	api.On("UpdatePreferencesForUser", "u1", mock.Anything).Return(func(_ string, prefs []model.Preference) *model.AppError {
		w.recent = prefs[0].Value
		return nil
	})
	api.On("KVGet", mock.Anything).Return(func(key string) []byte { return w.kv[key] }, nil)
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(
		func(key string, value []byte, _ model.PluginKVSetOptions) bool { w.kv[key] = value; return true }, nil)
	api.On("KVDelete", mock.Anything).Return(func(key string) *model.AppError { delete(w.kv, key); return nil })
	api.On("UpdateUserCustomStatus", "u1", mock.Anything).Return(func(_ string, cs *model.CustomStatus) *model.AppError {
		// Like the server: the status is validated and added to recent ones.
		cs.PreSave()
		if !cs.AreDurationAndExpirationTimeValid() {
			return model.NewAppError("UpdateUserCustomStatus", "invalid", nil, "", 400)
		}
		_ = w.user.SetCustomStatus(cs)
		data, _ := json.Marshal([]model.CustomStatus{*cs})
		w.recent = string(data)
		return nil
	})
	api.On("RemoveUserCustomStatus", "u1").Return(func(string) *model.AppError { w.user.ClearCustomStatus(); return nil })
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("GetConfig").Return(&model.Config{}).Maybe()
	p := &Plugin{config: &configuration{SetMeetingStatus: true}}
	p.SetAPI(api)
	return p, w
}

func TestMeetingStatusRestoresPrevious(t *testing.T) {
	previous := &model.CustomStatus{Emoji: "palm_tree", Text: "В отпуске"}
	p, w := newStatusWorld(t, previous)

	p.updatePresence("m1", nil, []string{"u1"})
	got := w.user.GetCustomStatus()
	if !isMeetingStatus(got) || got.Duration != "" || !got.ExpiresAt.IsZero() || !got.AreDurationAndExpirationTimeValid() {
		t.Fatalf("status during meeting must be a valid status with no end time: %+v", got)
	}
	if strings.Contains(w.recent, meetingStatusEmoji) {
		t.Fatalf("plugin status left in the recent list: %s", w.recent)
	}
	p.updatePresence("m1", []string{"u1"}, nil)
	if got := w.user.GetCustomStatus(); got == nil || got.Text != "В отпуске" {
		t.Fatalf("previous status not restored: %+v", got)
	}
	if _, left := w.kv[keyCustomStatus+"u1"]; left {
		t.Fatal("saved status left behind")
	}
	var ids []string
	_ = json.Unmarshal(w.kv[keyStatusIndex], &ids)
	if len(ids) != 0 {
		t.Fatalf("index still lists %v", ids)
	}
}

func TestMeetingStatusClearsWhenNothingBefore(t *testing.T) {
	p, w := newStatusWorld(t, nil)
	p.updatePresence("m1", nil, []string{"u1"})
	p.updatePresence("m1", []string{"u1"}, nil)
	if got := w.user.GetCustomStatus(); got != nil && got.Text != "" {
		t.Fatalf("status not cleared: %+v", got)
	}
}

func TestMeetingStatusRespectsUserChange(t *testing.T) {
	p, w := newStatusWorld(t, nil)
	p.updatePresence("m1", nil, []string{"u1"})
	_ = w.user.SetCustomStatus(&model.CustomStatus{Emoji: "coffee", Text: "Обед"})
	p.updatePresence("m1", []string{"u1"}, nil)
	if got := w.user.GetCustomStatus(); got == nil || got.Text != "Обед" {
		t.Fatalf("the user's own status was overwritten: %+v", got)
	}
}

func TestMeetingStatusHonoursPreference(t *testing.T) {
	p, w := newStatusWorld(t, nil)
	w.prefOn = "false"
	p.updatePresence("m1", nil, []string{"u1"})
	if got := w.user.GetCustomStatus(); got != nil && got.Text != "" {
		t.Fatalf("status set although the user turned it off: %+v", got)
	}
	p.config.SetMeetingStatus = false
	w.prefOn = "true"
	p.updatePresence("m1", nil, []string{"u1"})
	if got := w.user.GetCustomStatus(); got != nil && got.Text != "" {
		t.Fatalf("status set although the administrator turned it off: %+v", got)
	}
}

func TestMeetingStatusIgnoresOtherMeeting(t *testing.T) {
	p, w := newStatusWorld(t, nil)
	p.updatePresence("m1", nil, []string{"u1"})
	// Leaving another meeting must not clear the status of the current one.
	p.clearMeetingStatus("u1", "m2")
	if !isMeetingStatus(w.user.GetCustomStatus()) {
		t.Fatal("status cleared by an unrelated meeting")
	}
	var saved savedStatus
	_ = json.Unmarshal(w.kv[keyCustomStatus+"u1"], &saved)
	if saved.MeetingID != "m1" {
		t.Fatalf("saved for %q", saved.MeetingID)
	}
}

func TestSweepClearsStatusOfFinishedMeeting(t *testing.T) {
	p, w := newStatusWorld(t, &model.CustomStatus{Emoji: "palm_tree", Text: "В отпуске"})
	p.updatePresence("gone", nil, []string{"u1"})
	if !isMeetingStatus(w.user.GetCustomStatus()) {
		t.Fatal("status not set")
	}
	// The meeting record is gone and the end was never seen.
	p.sweepStatuses()
	if got := w.user.GetCustomStatus(); got == nil || got.Text != "В отпуске" {
		t.Fatalf("sweep did not restore: %+v", got)
	}
	var ids []string
	_ = json.Unmarshal(w.kv[keyStatusIndex], &ids)
	if len(ids) != 0 {
		t.Fatalf("index not emptied: %v", ids)
	}
}

func TestSweepKeepsStatusDuringMeeting(t *testing.T) {
	p, w := newStatusWorld(t, nil)
	live := &Meeting{ID: "m1", Status: statusLive, Present: []string{"u1"}}
	data, _ := json.Marshal(live)
	w.kv[keyMeeting+"m1"] = data
	p.updatePresence("m1", nil, []string{"u1"})
	p.sweepStatuses()
	if !isMeetingStatus(w.user.GetCustomStatus()) {
		t.Fatal("sweep cleared the status of a running meeting")
	}
}

func TestPluginEmojiCountsAsMeetingStatus(t *testing.T) {
	if !isMeetingStatus(&model.CustomStatus{Emoji: statusEmojiName, Text: meetingStatusTexts["ru"]}) {
		t.Fatal("status with the plugin emoji not recognised")
	}
	if isMeetingStatus(&model.CustomStatus{Emoji: statusEmojiName, Text: "Обед"}) {
		t.Fatal("someone else's status with the same emoji taken for ours")
	}
}
