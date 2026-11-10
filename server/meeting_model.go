package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

// Meeting kinds.
const (
	kindAdhoc     = "adhoc"
	kindCall      = "call"
	kindScheduled = "scheduled"
	kindPersonal  = "personal"
)

// Meeting statuses.
const (
	statusScheduled = "scheduled"
	statusRinging   = "ringing"
	statusWaiting   = "waiting"
	statusLive      = "live"
	statusEnded     = "ended"
	statusCancelled = "cancelled"
)

const (
	ringingTimeout   = 60 * time.Second
	unusedMeetingTTL = 24 * time.Hour
	// A call nobody took closes soon after the ringing stops, so it does not
	// hang among the meetings waiting for people. Someone who accepted gets
	// longer to open the room.
	missedCallGrace    = 2 * time.Minute
	acceptedCallGrace  = 10 * time.Minute
	emptyPollsToFinish = 2
	artifactInterval   = 2 * time.Minute
	attendeeInterval   = 15 * time.Minute
	noRecordingWindow  = 6 * time.Hour
	artifactWindow     = 24 * time.Hour
	scheduleGrace      = 12 * time.Hour
	joinOpensBefore    = 10 * time.Minute

	recordingSettleTime = time.Hour
)

// Callee statuses. Talk adds its own, such as doNotDisturb.
const (
	calleeCalling   = "calling"
	calleeAccepted  = "accepted"
	calleeDeclined  = "declined"
	calleeNoAnswer  = "no_answer"
	calleeCancelled = "cancelled"
	calleeCallsOff  = "calls_off"
)

// CallCallee is a person rung for a call.
type CallCallee struct {
	UserID  string `json:"user_id"`
	Name    string `json:"name"`
	TalkKey string `json:"talk_key,omitempty"`
	Status  string `json:"status"`
}

// MeetingAttendee is a calendar invitee with a response.
type MeetingAttendee struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	// Key is the Talk user key, empty for people outside Talk.
	Key string `json:"key,omitempty"`
}

// Schedule holds calendar data of a scheduled meeting.
type Schedule struct {
	Email          string    `json:"email"`
	UsePersonalKey bool      `json:"use_personal_key,omitempty"`
	EventID        string    `json:"event_id"`
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	Timezone       string    `json:"timezone,omitempty"`
	Description    string    `json:"description,omitempty"`
	Repeat         string    `json:"repeat,omitempty"`
	RepeatCount    int       `json:"repeat_count,omitempty"`
	// Rule is the full repeat; meetings planned before it existed have only
	// Repeat and RepeatCount.
	Rule *RepeatRule `json:"rule,omitempty"`
	// OrganizerKey is the organizer of the event as Talk reports it, read on
	// every calendar refresh. Cards use it; actions ask Talk again.
	OrganizerKey   string            `json:"organizer_key,omitempty"`
	Recurring      bool              `json:"recurring,omitempty"`
	UserIDs        []string          `json:"user_ids,omitempty"`
	UserKeys       []string          `json:"user_keys,omitempty"`
	ExternalEmails []string          `json:"external_emails,omitempty"`
	AllowGuests    bool              `json:"allow_guests,omitempty"`
	AutoRecord     bool              `json:"auto_record,omitempty"`
	EnableSIP      bool              `json:"enable_sip,omitempty"`
	Attendees      []MeetingAttendee `json:"attendees,omitempty"`
	// InviteeKeys maps invited Mattermost users to their Talk keys, so each
	// one can see the answer they gave.
	InviteeKeys map[string]string `json:"invitee_keys,omitempty"`
	Reminded    bool              `json:"reminded,omitempty"`
	AttendeesAt time.Time         `json:"attendees_at"`
}

// MeetingRecording tracks one recording of a meeting.
type MeetingRecording struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Duration    int    `json:"duration"`
	Posted      bool   `json:"posted,omitempty"`
	SummaryDone bool   `json:"summary_done,omitempty"`
}

// Who may open a recording started from Mattermost, as Talk words it.
const (
	accessSelf         = "self"         // only the person who started it
	accessParticipants = "participants" // those present: Talk grants them itself
	accessDomain       = "domain"       // anyone in the space with the link
	accessGlobal       = "global"       // anyone on the internet with the link
)

func validAccess(access string) bool {
	switch access {
	case accessSelf, accessParticipants, accessDomain, accessGlobal:
		return true
	}
	return false
}

// linkScope is the link access in Talk that an access choice starts with.
func linkScope(access string) string {
	if access == accessDomain || access == accessGlobal {
		return access
	}
	return "none"
}

// RecordingChoice is the access chosen when a recording was started.
type RecordingChoice struct {
	Access string `json:"access"`
	// Owner is the Talk user who keeps a recording limited to themselves.
	Owner string `json:"owner,omitempty"`
	// UserID is who chose, told if Talk refuses the access.
	UserID string `json:"user_id,omitempty"`
}

// MeetingResults are the materials of a finished conference.
type MeetingResults struct {
	URL      string   `json:"url,omitempty"`
	Attended []string `json:"attended,omitempty"`
	Missed   []string `json:"missed,omitempty"`
	Polls    int      `json:"polls,omitempty"`
	Notes    int      `json:"notes,omitempty"`
	Boards   int      `json:"boards,omitempty"`
}

// Meeting is the plugin's record of a Talk room used from a channel, stored in
// KV as is. Clients only ever receive the allowlisted view from view().
type Meeting struct {
	ID          string             `json:"id"`
	RoomName    string             `json:"room_name"`
	URL         string             `json:"url"`
	Title       string             `json:"title"`
	ChannelID   string             `json:"channel_id"`
	RootID      string             `json:"root_id,omitempty"`
	PostID      string             `json:"post_id"`
	CreatorID   string             `json:"creator_id"`
	CreatorName string             `json:"creator_name"`
	CreatorKey  string             `json:"creator_key,omitempty"`
	Kind        string             `json:"kind"`
	Status      string             `json:"status"`
	LinkOnly    bool               `json:"link_only,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	StartedAt   *time.Time         `json:"started_at,omitempty"`
	EndedAt     *time.Time         `json:"ended_at,omitempty"`
	UsersOnline int                `json:"users_online"`
	Online      []string           `json:"online,omitempty"`
	Present     []string           `json:"present,omitempty"`
	Moderators  []string           `json:"moderators,omitempty"`
	Pin         string             `json:"pin,omitempty"`
	GuestsUntil *time.Time         `json:"guests_until,omitempty"`
	Lobby       bool               `json:"lobby,omitempty"`
	Muted       bool               `json:"muted,omitempty"`
	SIP         *talk.SIPSettings  `json:"sip,omitempty"`
	Recording   bool               `json:"recording,omitempty"`
	Callees     []CallCallee       `json:"callees,omitempty"`
	Schedule    *Schedule          `json:"schedule,omitempty"`
	Results     *MeetingResults    `json:"results,omitempty"`
	Recordings  []MeetingRecording `json:"recordings,omitempty"`
	// RecordingChoices are the accesses chosen for recordings started from
	// Mattermost, by recording key. They stay on the server.
	RecordingChoices map[string]RecordingChoice `json:"recording_choices,omitempty"`
	ConferenceKey    string                     `json:"conference_key,omitempty"`
	RingingUntil     time.Time                  `json:"ringing_until"`
	EmptyPolls       int                        `json:"empty_polls,omitempty"`
	ArtifactPollAt   time.Time                  `json:"artifact_poll_at"`
	Expired          bool                       `json:"expired,omitempty"`
}

// view returns what clients may see. It is an allowlist: a new Meeting field
// stays server-only until it is added here. The result holds only plain JSON
// values, because post props cross the plugin RPC boundary through gob, which
// panics on unregistered types and on nil interface values.
func (m *Meeting) view() map[string]any {
	return plainMap(m.rawView())
}

func plainMap(value map[string]any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	plain := map[string]any{}
	if err := json.Unmarshal(data, &plain); err != nil {
		return map[string]any{}
	}
	stripNulls(plain)
	return plain
}

func stripNulls(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if item == nil {
				delete(typed, key)
				continue
			}
			stripNulls(item)
		}
	case []any:
		for _, item := range typed {
			stripNulls(item)
		}
	}
}

func (m *Meeting) rawView() map[string]any {
	v := map[string]any{
		"id": m.ID, "room_name": m.RoomName, "url": m.URL, "title": m.Title,
		"channel_id": m.ChannelID, "post_id": m.PostID, "creator_id": m.CreatorID,
		"creator_name": m.CreatorName, "kind": m.Kind, "status": m.Status, "link_only": m.LinkOnly,
		"created_at": m.CreatedAt, "started_at": m.StartedAt, "ended_at": m.EndedAt,
		"users_online": m.UsersOnline, "online": m.Online, "pin": m.Pin, "guests_until": m.GuestsUntil,
		"lobby": m.Lobby, "sip": m.SIP, "recording": m.Recording, "results": m.Results, "expired": m.Expired,
	}
	if len(m.Callees) > 0 {
		callees := make([]map[string]any, 0, len(m.Callees))
		for _, c := range m.Callees {
			callees = append(callees, map[string]any{"user_id": c.UserID, "name": c.Name, "status": c.Status})
		}
		v["callees"] = callees
	}
	if len(m.Recordings) > 0 {
		recordings := make([]map[string]any, 0, len(m.Recordings))
		for _, r := range m.Recordings {
			recordings = append(recordings, map[string]any{"key": r.Key, "title": r.Title, "url": r.URL, "duration": r.Duration})
		}
		v["recordings"] = recordings
	}
	if sc := m.Schedule; sc != nil {
		v["schedule"] = map[string]any{
			"start": sc.Start, "end": sc.End, "description": sc.Description, "repeat": sc.Repeat, "repeat_text": sc.repeatText(),
			"recurring": sc.Recurring, "auto_record": sc.AutoRecord, "attendees": sc.Attendees,
			// Mattermost users invited, so their clients can offer to answer.
			"invited": sc.UserIDs,
			"answers": sc.answers(),
			// Talk keys of invited users, so a client finds its own attendee row.
			"keys": sc.InviteeKeys,
		}
	}
	return v
}

// threadRoot returns the post replies about this meeting belong to.
func (m *Meeting) threadRoot() string {
	if m.RootID != "" {
		return m.RootID
	}
	return m.PostID
}

func (m *Meeting) isOpen() bool {
	return m.Status != statusEnded && m.Status != statusCancelled
}

// applyRoomState moves a meeting through its statuses using a room snapshot.
// It is pure so the state machine can be tested without any server. The return
// value reports that the conference has just finished.
func applyRoomState(m *Meeting, room *talk.Room, now time.Time) (finished bool) {
	m.UsersOnline = room.UsersOnline
	m.Online = onlineNames(room, 12)
	if room.SIPSettings != nil && (room.SIPSettings.SIPNumber != "" || len(room.SIPSettings.PhoneNumbers) > 0) {
		m.SIP = room.SIPSettings
	}

	if room.UsersOnline > 0 {
		m.EmptyPolls = 0
		// Anyone still being rung when the call is answered stops ringing.
		for i := range m.Callees {
			if m.Callees[i].Status == calleeCalling && m.Status == statusRinging && room.UsersOnline > 1 {
				m.Callees[i].Status = calleeNoAnswer
			}
		}
		if m.Status != statusLive {
			m.Status = statusLive
			started := now
			m.StartedAt = &started
			m.EndedAt = nil
		}
		return false
	}

	switch m.Status {
	case statusLive:
		// Reconnects briefly empty a room, so one empty poll proves nothing.
		m.EmptyPolls++
		if m.EmptyPolls < emptyPollsToFinish {
			return false
		}
		ended := now
		m.EndedAt = &ended
		m.Status = statusEnded
		m.Recording = false
		return true
	case statusRinging:
		if now.After(m.RingingUntil) {
			m.Status = statusWaiting
			for i := range m.Callees {
				if m.Callees[i].Status == calleeCalling {
					m.Callees[i].Status = calleeNoAnswer
				}
			}
		}
	case statusScheduled:
		if m.Schedule != nil && now.After(m.Schedule.End.Add(scheduleGrace)) {
			expireMeeting(m, now)
		}
	case statusWaiting:
		if now.After(waitingDeadline(m)) {
			expireMeeting(m, now)
		}
	}
	return false
}

// idleListing is how long a link nobody has opened stays among current
// meetings. The room keeps working and is still watched until
// unusedMeetingTTL, so a late join brings the meeting back to the list.
const idleListing = 2 * time.Hour

// idle tells that a meeting nobody has joined has waited too long to be
// listed as current. Calls have their own short deadline.
func (m *Meeting) idle(now time.Time) bool {
	return m.Status == statusWaiting && m.Kind != kindCall && m.StartedAt == nil && now.Sub(m.CreatedAt) > idleListing
}

// waitingDeadline is when an empty meeting that nobody has joined closes.
func waitingDeadline(m *Meeting) time.Time {
	if m.Kind != kindCall {
		return m.CreatedAt.Add(unusedMeetingTTL)
	}
	stopped := m.RingingUntil
	if stopped.Before(m.CreatedAt) {
		stopped = m.CreatedAt
	}
	for _, callee := range m.Callees {
		if callee.Status == calleeAccepted {
			return stopped.Add(acceptedCallGrace)
		}
	}
	return stopped.Add(missedCallGrace)
}

func expireMeeting(m *Meeting, now time.Time) {
	ended := now
	m.EndedAt = &ended
	m.Status = statusEnded
	m.Expired = true
}

func onlineNames(room *talk.Room, limit int) []string {
	names := make([]string, 0, limit)
	for i := range room.OnlineUsers {
		if len(names) == limit {
			break
		}
		if name := room.OnlineUsers[i].Name(); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// needsResults reports that a finished conference has no results on its card.
// A meeting nobody joined has none to wait for.
func needsResults(m *Meeting) bool {
	return m.Status == statusEnded && !m.Expired && m.Results == nil
}

// recordingReady reports that a recording can be announced. Talk lists a
// recording as soon as it starts, with zero duration, so a listed recording may
// still be in progress. One without duration long after the meeting ended will
// never get any, and is announced anyway.
func recordingReady(m *Meeting, rec *talk.Recording, now time.Time) bool {
	if m.Status == statusLive && m.Recording {
		return false
	}
	if rec.Duration > 0 {
		return true
	}
	return m.EndedAt != nil && now.Sub(*m.EndedAt) > recordingSettleTime
}

// summaryIsFinal reports that a summary status will not change. "notFound" and
// "notAvailable" are not final: they are also what a recording reports before
// its transcription has started. Those are retried until the artifact window
// closes.
func summaryIsFinal(status string) bool {
	return status == talk.SpeechSuccess || status == "failed"
}

// artifactsSettled reports that a finished meeting needs no more polling.
func artifactsSettled(m *Meeting, now time.Time) bool {
	if m.EndedAt == nil || m.Status != statusEnded {
		return false
	}
	if m.Expired {
		return true
	}
	age := now.Sub(*m.EndedAt)
	if age > artifactWindow {
		return true
	}
	if len(m.Recordings) == 0 {
		return age > noRecordingWindow
	}
	for i := range m.Recordings {
		if !m.Recordings[i].SummaryDone {
			return false
		}
	}
	return true
}

// roomPolicy decides room settings by channel type.
type roomPolicy struct {
	Lobby       bool
	GuestsUntil *time.Time
	Muted       bool
}

func computeRoomPolicy(channelType string, memberCount int64, cfg *configuration, now time.Time) roomPolicy {
	switch channelType {
	case "D", "G":
		return roomPolicy{}
	case "P":
		return roomPolicy{Lobby: true}
	}
	policy := roomPolicy{Lobby: cfg.PublicLobby, Muted: memberCount > int64(cfg.MuteAboveMembers)}
	if cfg.PublicGuestHours > 0 {
		until := now.Add(time.Duration(cfg.PublicGuestHours) * time.Hour)
		policy.GuestsUntil = &until
	}
	return policy
}

// timezoneLabel renders a location offset in the GMT+X form the calendar wants.
func timezoneLabel(t time.Time) string {
	_, offset := t.Zone()
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	if minutes < 0 {
		minutes = -minutes
	}
	if minutes != 0 {
		return fmt.Sprintf("GMT%+d:%02d", hours, minutes)
	}
	return fmt.Sprintf("GMT%+d", hours)
}

// safeInline stops user or Talk text in bot posts from triggering mentions.
// markdownEscaper neutralizes text placed inside Markdown the plugin writes.
// Mentions are broken with a zero-width space; link and emphasis marks are
// escaped, so a title such as "x](https://evil) [y" cannot smuggle a link.
var markdownEscaper = strings.NewReplacer("@", "@\u200b", "\\", "\\\\", "[", "\\[", "]", "\\]", "`", "\\`", "*", "\\*", "_", "\\_", "~", "\\~")

func safeInline(s string) string {
	return markdownEscaper.Replace(s)
}

func clip(s string, limit int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= limit {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

// answers is the response of each invited Mattermost user who is in Talk.
func (sc *Schedule) answers() map[string]string {
	byKey := map[string]string{}
	for _, attendee := range sc.Attendees {
		if attendee.Key != "" {
			byKey[attendee.Key] = attendee.Status
		}
	}
	out := map[string]string{}
	for userID, key := range sc.InviteeKeys {
		if status, ok := byKey[key]; ok {
			out[userID] = status
		}
	}
	return out
}

// repeatText describes how the meeting repeats, "" for a single meeting.
// Meetings planned before rules existed are described by their period.
func (sc *Schedule) repeatText() string {
	if sc.Rule != nil {
		return sc.Rule.Text()
	}
	if !sc.Recurring || sc.Repeat == "" {
		return ""
	}
	return legacyRule(sc.Repeat, sc.RepeatCount, sc.Start).Text()
}
