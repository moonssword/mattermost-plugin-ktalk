package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/mock"
)

// callWorld is a fake server with one ringing call from u1 to u2 and u3.
func callWorld(t *testing.T) (*Plugin, map[string][]byte, *[]string) {
	t.Helper()
	kv := map[string][]byte{}
	m := &Meeting{ID: "m1", ChannelID: "c1", PostID: "p1", CreatorID: "u1", Kind: kindCall, Status: statusRinging,
		Callees: []CallCallee{{UserID: "u2", Status: calleeCalling}, {UserID: "u3", Status: calleeCalling}}}
	data, _ := json.Marshal(m)
	kv[keyMeeting+"m1"] = data

	dismissed := []string{}
	api := &plugintest.API{}
	api.On("KVGet", mock.Anything).Return(func(key string) []byte { return kv[key] }, nil)
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(
		func(key string, value []byte, _ model.PluginKVSetOptions) bool { kv[key] = value; return true }, nil)
	api.On("HasPermissionToChannel", mock.Anything, "c1", model.PermissionReadChannel).Return(true)
	api.On("HasPermissionTo", mock.Anything, model.PermissionManageSystem).Return(false)
	api.On("GetChannelMember", "c1", mock.Anything).Return(&model.ChannelMember{}, nil)
	api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		if args.String(0) == wsDismissCall {
			dismissed = append(dismissed, args.Get(2).(*model.WebsocketBroadcast).UserId)
		}
	})
	api.On("GetPost", "p1").Return(&model.Post{Id: "p1"}, nil)
	api.On("UpdatePost", mock.Anything).Return(&model.Post{}, nil)
	p := &Plugin{config: &configuration{}}
	p.SetAPI(api)
	return p, kv, &dismissed
}

func stored(t *testing.T, kv map[string][]byte) *Meeting {
	t.Helper()
	var m Meeting
	if err := json.Unmarshal(kv[keyMeeting+"m1"], &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

func TestDeclineStopsOnlyThatCallee(t *testing.T) {
	p, kv, dismissed := callWorld(t)
	if err := p.answerCall("u2", "m1", false); err != nil {
		t.Fatal(err)
	}
	m := stored(t, kv)
	if m.Callees[0].Status != calleeDeclined || m.Callees[1].Status != calleeCalling || m.Status != statusRinging {
		t.Fatalf("after decline: %+v", m)
	}
	if len(*dismissed) != 1 || (*dismissed)[0] != "u2" {
		t.Fatalf("dismissed %v", *dismissed)
	}
}

func TestLastAnswerEndsRinging(t *testing.T) {
	p, kv, _ := callWorld(t)
	_ = p.answerCall("u2", "m1", true)
	_ = p.answerCall("u3", "m1", false)
	m := stored(t, kv)
	if m.Callees[0].Status != calleeAccepted || m.Callees[1].Status != calleeDeclined || m.Status != statusWaiting {
		t.Fatalf("after both answered: %+v", m)
	}
}

func TestAnswerTwiceIsHarmless(t *testing.T) {
	p, kv, _ := callWorld(t)
	_ = p.answerCall("u2", "m1", true)
	_ = p.answerCall("u2", "m1", false)
	if got := stored(t, kv).Callees[0].Status; got != calleeAccepted {
		t.Fatalf("second answer overwrote the first: %s", got)
	}
}

func TestCancelSilencesEveryoneStillRinging(t *testing.T) {
	p, kv, dismissed := callWorld(t)
	_ = p.answerCall("u2", "m1", true)
	*dismissed = nil
	if err := p.cancelRinging("u1", "m1"); err != nil {
		t.Fatal(err)
	}
	m := stored(t, kv)
	if m.Callees[1].Status != calleeCancelled || m.Callees[0].Status != calleeAccepted || m.Status != statusWaiting {
		t.Fatalf("after cancel: %+v", m)
	}
	if len(*dismissed) != 1 || (*dismissed)[0] != "u3" {
		t.Fatalf("dismissed %v", *dismissed)
	}
}

func TestUpcomingEditableOnlyForController(t *testing.T) {
	p, kv, _ := callWorld(t)
	m := stored(t, kv)
	m.Kind, m.Status, m.RoomName, m.Callees = kindAdhoc, statusScheduled, "room1", nil
	m.Schedule = &Schedule{EventID: "e1"}
	data, _ := json.Marshal(m)
	kv[keyMeeting+"m1"] = data
	if err := p.kvSet(keyRoom+"room1", "m1", 0); err != nil {
		t.Fatal(err)
	}

	event := upcomingEvent{ID: "e1"}
	p.attachEditable(&event, "room1", "u1")
	if event.MeetingID != "m1" || event.ChannelID != "c1" {
		t.Fatalf("author cannot edit: %+v", event)
	}
	other := upcomingEvent{ID: "e1"}
	p.attachEditable(&other, "room1", "u2")
	if other.MeetingID != "" {
		t.Fatal("a plain member got the edit button")
	}
	foreign := upcomingEvent{ID: "e2"}
	p.attachEditable(&foreign, "elsewhere", "u1")
	if foreign.MeetingID != "" {
		t.Fatal("a meeting planned outside Mattermost became editable")
	}
}

func TestInvitationAnswerNeedsInviteAndKey(t *testing.T) {
	p, kv, _ := callWorld(t)
	m := stored(t, kv)
	m.Kind, m.Status, m.Callees = kindAdhoc, statusScheduled, nil
	m.Schedule = &Schedule{EventID: "e1", UserIDs: []string{"u2"}}
	data, _ := json.Marshal(m)
	kv[keyMeeting+"m1"] = data

	var notInvited *userError
	if err := p.respondInvitation("u3", "m1", true); !errors.As(err, &notInvited) || notInvited.status != http.StatusForbidden {
		t.Fatalf("someone not invited could answer: %v", err)
	}
	var noKey *userError
	if err := p.respondInvitation("u2", "m1", true); !errors.As(err, &noKey) || noKey.code != codePersonalKey {
		t.Fatalf("answer without a personal key: %v", err)
	}
}
