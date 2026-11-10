package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

// Who may do what with a meeting is decided by Talk, never by Mattermost
// roles: neither channel admins nor system admins get anything extra.
//
//   - The room is run by its moderators in Talk: recording, ending for
//     everyone, the PIN, guest access and the waiting room.
//   - A scheduled meeting is changed or cancelled by the organizer of its
//     calendar event in Talk.
//   - An invitation is answered by attendees of that event in Talk.
//   - Ringing exists only in Mattermost, so the person who rings stops it.
type meetingRight int

const (
	rightRoom meetingRight = iota
	rightSchedule
	rightRing
)

// talkKeyOf is the Talk user a Mattermost user acts as, "" when the address
// is unverified or not found in Talk.
func (p *Plugin) talkKeyOf(userID string) string {
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return ""
	}
	identity, ok := p.talkUser(user)
	if !ok {
		return ""
	}
	return identity.Key
}

// isModerator asks Talk whether a Talk user moderates a room.
func isModerator(ctx context.Context, client *talk.Client, roomName, key string) (bool, error) {
	room, err := client.GetRoom(ctx, roomName)
	if err != nil {
		return false, err
	}
	for i := range room.Moderators {
		if room.Moderators[i].Key == key {
			return true, nil
		}
	}
	return false, nil
}

// organizerOf reads a scheduled meeting's event from Talk and returns its
// organizer's key, with the event for callers that need its attendees.
func (p *Plugin) organizerOf(ctx context.Context, m *Meeting) (string, *talk.CalendarItem, error) {
	client, err := p.calendarClientFor(m)
	if err != nil {
		return "", nil, err
	}
	item, err := p.calendarItem(ctx, client, m)
	if err != nil {
		return "", nil, err
	}
	return organizerKey(item), item, nil
}

func organizerKey(item *talk.CalendarItem) string {
	if item == nil || item.Organizer == nil || item.Organizer.User == nil {
		return ""
	}
	return item.Organizer.User.Key
}

// hasRight checks a right against Talk at the moment of asking. Any doubt,
// a Talk error included, means no.
func (p *Plugin) hasRight(userID string, m *Meeting, right meetingRight) bool {
	if right == rightRing {
		return userID == m.CreatorID
	}
	if m.LinkOnly {
		return false
	}
	key := p.talkKeyOf(userID)
	client := p.client()
	if key == "" || client == nil {
		return false
	}
	ctx, cancel := apiContext()
	defer cancel()
	switch right {
	case rightRoom:
		ok, err := isModerator(ctx, client, m.RoomName, key)
		if err != nil {
			p.noteAPIError(err)
			p.API.LogWarn("Room moderators unavailable", "meeting_id", m.ID, "error", err.Error())
		}
		return ok
	case rightSchedule:
		if m.Schedule == nil {
			return false
		}
		organizer, _, err := p.organizerOf(ctx, m)
		if err != nil {
			p.noteAPIError(err)
			p.API.LogWarn("Event organizer unavailable", "meeting_id", m.ID, "error", err.Error())
		}
		return organizer != "" && organizer == key
	}
	return false
}

// controlledMeeting loads a meeting the user may act on with the given right.
func (p *Plugin) controlledMeeting(userID, meetingID string, right meetingRight) (*Meeting, error) {
	m, err := p.getMeeting(meetingID)
	if errors.Is(err, errMeetingNotFound) {
		return nil, newUserError(http.StatusNotFound, "Встреча не найдена.")
	}
	if err != nil {
		return nil, err
	}
	if m.LinkOnly && right != rightRing {
		return nil, newUserError(http.StatusConflict, "Встреча создана без API-ключа: управлять ей можно только в Толке.")
	}
	if !p.hasRight(userID, m, right) {
		return nil, errForbidden
	}
	return m, nil
}

// meetingRights is what a card may offer one user. It mirrors hasRight, but
// for a scheduled meeting reads the organizer and attendees the plugin last
// took from Talk, so drawing a card costs no calendar call. Every action
// still asks Talk itself.
func (p *Plugin) meetingRights(userID string, m *Meeting) map[string]any {
	key := p.talkKeyOf(userID)
	rights := map[string]any{"room": false, "schedule": false, "ring": userID == m.CreatorID && m.Status == statusRinging, "attendee": false, "key": key}
	if key == "" || m.LinkOnly || !m.isOpen() {
		return rights
	}
	if sc := m.Schedule; sc != nil && m.Status == statusScheduled {
		organizer := sc.OrganizerKey
		rights["schedule"] = organizer != "" && organizer == key
		for _, attendee := range sc.Attendees {
			if attendee.Key == key && key != organizer {
				rights["attendee"] = true
			}
		}
	}
	if m.Status == statusLive || m.Status == statusWaiting || m.Status == statusRinging {
		rights["room"] = p.hasRight(userID, m, rightRoom)
	}
	return rights
}
