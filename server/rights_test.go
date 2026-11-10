package main

import (
	"encoding/json"
	"errors"
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

// rightsWorld is a scheduled meeting in channel c1. In Talk, "owner"
// organizes its event and moderates its room; "guest" is invited. "sysadmin"
// is a Mattermost system admin and "chanadmin" an admin of the channel, and
// neither takes part in Talk, so neither may do anything.
func rightsWorld(t *testing.T) *Plugin {
	t.Helper()
	start := time.Date(2026, 11, 20, 7, 0, 0, 0, time.UTC)
	talkAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/Rooms/room1":
			_, _ = w.Write([]byte(`{"roomName":"room1","moderators":[{"key":"k-owner"}]}`))
		case strings.HasPrefix(r.URL.Path, "/api/EmailCalendar/owner@example.com"):
			_, _ = w.Write([]byte(`{"items":[{"id":"e1","roomName":"room1","start":"2026-11-20T07:00:00Z","end":"2026-11-20T08:00:00Z",
				"organizer":{"user":{"key":"k-owner"}},
				"requiredAttendees":[{"user":{"key":"k-owner"},"status":"accepted"},{"user":{"key":"k-guest"},"status":"unknown"}]}]}`))
		default:
			http.Error(w, "unknown", http.StatusNotFound)
		}
	}))
	t.Cleanup(talkAPI.Close)

	kv := map[string][]byte{}
	m := &Meeting{ID: "m1", ChannelID: "c1", CreatorID: "owner", RoomName: "room1", Status: statusScheduled, Kind: kindScheduled,
		Schedule: &Schedule{Email: "owner@example.com", EventID: "e1", Start: start, End: start.Add(time.Hour), OrganizerKey: "k-owner",
			Attendees: []MeetingAttendee{{Key: "k-owner", Status: "accepted"}, {Key: "k-guest", Status: "unknown"}}}}
	kv[keyMeeting+"m1"], _ = json.Marshal(m)
	kv[keyRoom+"room1"], _ = json.Marshal("m1")
	api := &plugintest.API{}
	for id, key := range map[string]string{"owner": "k-owner", "guest": "k-guest", "sysadmin": "k-sys", "chanadmin": "k-chan"} {
		user := &model.User{Id: id, Email: id + "@example.com", EmailVerified: true}
		if id == "sysadmin" {
			user.Roles = model.SystemAdminRoleId + " " + model.SystemUserRoleId
		}
		api.On("GetUser", id).Return(user, nil)
		kv[keyUserMap+id], _ = json.Marshal(talkIdentity{Email: user.Email, Key: key})
	}
	api.On("KVGet", mock.Anything).Return(func(key string) []byte { return kv[key] }, nil)
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(
		func(key string, value []byte, _ model.PluginKVSetOptions) bool { kv[key] = value; return true }, nil)
	// Mattermost would grant both admins everything; the plugin must not ask.
	api.On("HasPermissionTo", mock.Anything, mock.Anything).Return(true).Maybe()
	api.On("GetChannelMember", "c1", mock.Anything).Return(&model.ChannelMember{SchemeAdmin: true}, nil).Maybe()
	for _, level := range []string{"LogDebug", "LogInfo", "LogWarn", "LogError"} {
		api.On(level, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On(level, mock.Anything, mock.Anything, mock.Anything).Maybe()
	}
	p := &Plugin{config: &configuration{SpaceURL: talkAPI.URL, APIKey: "key", RequireVerifiedEmail: true}}
	p.SetAPI(api)
	return p
}

func TestRightsComeFromTalkOnly(t *testing.T) {
	p := rightsWorld(t)
	m, _ := p.getMeeting("m1")
	cases := []struct {
		user           string
		room, schedule bool
	}{
		{"owner", true, true},
		{"guest", false, false},
		{"sysadmin", false, false},
		{"chanadmin", false, false},
	}
	for _, c := range cases {
		if got := p.hasRight(c.user, m, rightRoom); got != c.room {
			t.Errorf("%s room: %v", c.user, got)
		}
		if got := p.hasRight(c.user, m, rightSchedule); got != c.schedule {
			t.Errorf("%s schedule: %v", c.user, got)
		}
	}
	if _, err := p.controlledMeeting("sysadmin", "m1", rightSchedule); !errors.Is(err, errForbidden) {
		t.Errorf("a system admin may edit: %v", err)
	}
}

func TestCardRightsFollowTalk(t *testing.T) {
	p := rightsWorld(t)
	m, _ := p.getMeeting("m1")
	owner, guest, admin := p.meetingRights("owner", m), p.meetingRights("guest", m), p.meetingRights("sysadmin", m)
	if owner["schedule"] != true || owner["attendee"] != false {
		t.Errorf("owner %v", owner)
	}
	if guest["schedule"] != false || guest["attendee"] != true || guest["key"] != "k-guest" {
		t.Errorf("guest %v", guest)
	}
	if admin["schedule"] != false || admin["attendee"] != false || admin["room"] != false {
		t.Errorf("system admin %v", admin)
	}
}

func TestUpcomingEditableOnlyForTheOrganizer(t *testing.T) {
	p := rightsWorld(t)
	item := &talk.CalendarItem{ID: "e1", RoomName: "room1", Organizer: &talk.Attendee{User: &talk.User{Key: "k-owner"}}}
	event := upcomingEvent{ID: "e1"}
	p.attachEditable(&event, item, "k-owner")
	if event.MeetingID != "m1" || event.ChannelID != "c1" {
		t.Fatalf("the organizer cannot edit: %+v", event)
	}
	other := upcomingEvent{ID: "e1"}
	p.attachEditable(&other, item, "k-guest")
	if other.MeetingID != "" {
		t.Fatal("an attendee got the edit button")
	}
	foreign := upcomingEvent{ID: "e2"}
	p.attachEditable(&foreign, &talk.CalendarItem{ID: "e2", RoomName: "elsewhere", Organizer: item.Organizer}, "k-owner")
	if foreign.MeetingID != "" {
		t.Fatal("a meeting planned outside Mattermost became editable")
	}
}

func TestInvitationAnswerFollowsTalkAttendees(t *testing.T) {
	p := rightsWorld(t)
	p.config.AllowPersonalKeys = true
	var noKey *userError
	if err := p.respondInvitation("guest", "m1", true); !errors.As(err, &noKey) || noKey.code != codePersonalKey {
		t.Fatalf("answer without a personal key: %v", err)
	}
	// With a key, only an attendee in Talk passes; the organizer and
	// Mattermost admins do not.
	for _, user := range []string{"owner", "sysadmin", "chanadmin"} {
		encrypted, err := p.encrypt("token-" + user)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.kvSet(keyPAT+user, personalKey{Token: encrypted}, 0); err != nil {
			t.Fatal(err)
		}
		var refused *userError
		if err := p.respondInvitation(user, "m1", true); !errors.As(err, &refused) || refused.status != http.StatusForbidden {
			t.Errorf("%s could answer: %v", user, err)
		}
	}
}
