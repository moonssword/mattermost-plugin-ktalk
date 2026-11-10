package main

import (
	"testing"

	"github.com/stretchr/testify/mock"
)

func TestKeyUsageCountsUsersOfThePlugin(t *testing.T) {
	p := rightsWorld(t)
	api := p.API.(interface {
		On(string, ...any) *mock.Call
	})
	for range 2 {
		p.markSeen("guest")
	}
	// guest opened the plugin; owner and chanadmin have keys; m1 is a meeting.
	api.On("KVList", 0, 1000).Return([]string{keySeen + "guest", keySeen + "owner", keyPAT + "owner", keyPAT + "chanadmin", keyMeeting + "m1"}, nil)
	withKey, users, err := p.keyUsage()
	if err != nil || withKey != 2 || users != 3 {
		t.Fatalf("withKey=%d users=%d err=%v", withKey, users, err)
	}
	var seen int
	if found, _ := p.kvGet(keySeen+"guest", &seen); !found {
		t.Fatal("the user was not marked")
	}
}
