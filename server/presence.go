package main

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

const (
	keyCustomStatus = "cstatus:"
	keyTalkKey      = "tkey:"

	keyStatusIndex = "cstatus_index"

	// The status shows the plugin's own emoji when an administrator's client has
	// registered it (see the webapp), and a standard camera until then.
	meetingStatusEmoji = "video_camera"
	statusEmojiName    = "konturtalk"
	// No end time is shown: a call has none in advance, and a guessed one
	// misleads. Statuses the plugin set are cleaned up by sweepStatuses instead.
	// Mattermost stores "do not clear" as an empty duration and a zero time;
	// any other spelling passes the plugin API but breaks the status dialog.
	meetingStatusDuration = ""
	savedStatusTTL        = 7 * 24 * time.Hour
	prefMeetingStatus     = "meeting_status"

	prefCustomStatusCategory = "custom_status"
	prefRecentCustomStatuses = "recent_custom_statuses"
)

var meetingStatusTexts = map[string]string{
	"ru": "На встрече в Контур.Толке",
	"en": "In a Kontur Talk meeting",
}

func meetingStatusText(locale string) string {
	if strings.HasPrefix(locale, "ru") || locale == "" {
		return meetingStatusTexts["ru"]
	}
	return meetingStatusTexts["en"]
}

// isMeetingStatus reports whether a custom status is the one this plugin sets.
func isMeetingStatus(status *model.CustomStatus) bool {
	if status == nil || (status.Emoji != meetingStatusEmoji && status.Emoji != statusEmojiName) {
		return false
	}
	for _, text := range meetingStatusTexts {
		if status.Text == text {
			return true
		}
	}
	return false
}

// savedStatus remembers what a user's custom status was before the meeting.
type savedStatus struct {
	MeetingID string              `json:"meeting_id"`
	Previous  *model.CustomStatus `json:"previous,omitempty"`
}

func (p *Plugin) meetingStatus(locale string) *model.CustomStatus {
	return &model.CustomStatus{Emoji: p.statusEmoji(), Text: meetingStatusText(locale), Duration: meetingStatusDuration}
}

// statusEmoji uses the plugin's emoji when it exists on the server.
func (p *Plugin) statusEmoji() string {
	if cfg := p.API.GetConfig(); cfg == nil || cfg.ServiceSettings.EnableCustomEmoji == nil || !*cfg.ServiceSettings.EnableCustomEmoji {
		return meetingStatusEmoji
	}
	if _, appErr := p.API.GetEmojiByName(statusEmojiName); appErr != nil {
		return meetingStatusEmoji
	}
	return statusEmojiName
}

// diffPresence splits users into those who joined and those who left.
func diffPresence(before, now []string) (joined, left []string) {
	was := map[string]bool{}
	for _, id := range before {
		was[id] = true
	}
	is := map[string]bool{}
	for _, id := range now {
		is[id] = true
		if !was[id] {
			joined = append(joined, id)
		}
	}
	for _, id := range before {
		if !is[id] {
			left = append(left, id)
		}
	}
	return joined, left
}

// presentUsers maps the registered people in a room to Mattermost users. An
// address only counts when it may be trusted, as everywhere else in the plugin.
func (p *Plugin) presentUsers(room *talk.Room) []string {
	ids := []string{}
	seen := map[string]bool{}
	for i := range room.OnlineUsers {
		info := room.OnlineUsers[i].UserInfo
		if info == nil || room.OnlineUsers[i].IsAnonymous {
			continue
		}
		email := strings.ToLower(info.Email)
		if email == "" && info.Key != "" {
			email = p.emailForTalkKey(info.Key)
		}
		if email == "" {
			continue
		}
		user, appErr := p.API.GetUserByEmail(email)
		if appErr != nil || user.IsBot || p.talkEmail(user) != email || seen[user.Id] {
			continue
		}
		seen[user.Id] = true
		ids = append(ids, user.Id)
	}
	return ids
}

// emailForTalkKey looks up an address when a room lists a user without one.
func (p *Plugin) emailForTalkKey(key string) string {
	var email string
	if found, _ := p.kvGet(keyTalkKey+key, &email); found {
		return email
	}
	client := p.client()
	if client == nil {
		return ""
	}
	ctx, cancel := apiContext()
	defer cancel()
	user, err := client.GetUser(ctx, key)
	if err != nil {
		p.API.LogDebug("Talk user lookup by key failed", "error", err.Error())
		return ""
	}
	email = strings.ToLower(user.Email)
	if err := p.kvSet(keyTalkKey+key, email, userMapTTL); err != nil {
		p.API.LogDebug("Talk key cache write failed", "error", err.Error())
	}
	return email
}

// updatePresence sets the status of people who joined and restores it for
// people who left. Statuses follow the meeting, so they are best effort and
// never stop a sync.
func (p *Plugin) updatePresence(meetingID string, before, now []string) {
	joined, left := diffPresence(before, now)
	for _, userID := range joined {
		p.setMeetingStatus(userID, meetingID)
	}
	for _, userID := range left {
		p.clearMeetingStatus(userID, meetingID)
	}
}

func (p *Plugin) wantsMeetingStatus(userID string) bool {
	return p.getConfig().SetMeetingStatus && p.userPref(userID, prefMeetingStatus, "true") == "true"
}

func (p *Plugin) setMeetingStatus(userID, meetingID string) {
	if !p.wantsMeetingStatus(userID) {
		return
	}
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return
	}
	var saved savedStatus
	if found, _ := p.kvGet(keyCustomStatus+userID, &saved); found {
		// Already in a meeting; the status moves to the newest one.
		saved.MeetingID = meetingID
		_ = p.kvSet(keyCustomStatus+userID, saved, savedStatusTTL)
		return
	}
	current := user.GetCustomStatus()
	if current != nil && !current.ExpiresAt.IsZero() && current.ExpiresAt.Before(time.Now()) {
		current = nil
	}
	if isMeetingStatus(current) {
		current = nil
	}
	// Remember first, so a crash between the two steps cannot strand a status
	// the plugin no longer knows it set.
	if err := p.kvSet(keyCustomStatus+userID, savedStatus{MeetingID: meetingID, Previous: current}, savedStatusTTL); err != nil {
		p.API.LogWarn("Failed to remember previous status", "user_id", userID, "error", err.Error())
		return
	}
	if err := p.indexStatus(userID, true); err != nil {
		p.API.LogWarn("Failed to index meeting status", "user_id", userID, "error", err.Error())
	}
	if appErr := p.API.UpdateUserCustomStatus(userID, p.meetingStatus(user.Locale)); appErr != nil {
		p.API.LogWarn("Failed to set meeting status", "user_id", userID, "error", appErr.Error())
		return
	}
	p.forgetRecentMeetingStatus(userID)
}

// forgetRecentMeetingStatus removes the plugin's status from the list of
// recent statuses Mattermost offers the user: the user never chose it.
func (p *Plugin) forgetRecentMeetingStatus(userID string) {
	pref, appErr := p.API.GetPreferenceForUser(userID, prefCustomStatusCategory, prefRecentCustomStatuses)
	if appErr != nil || pref.Value == "" {
		return
	}
	var recent []model.CustomStatus
	if err := json.Unmarshal([]byte(pref.Value), &recent); err != nil {
		return
	}
	kept := make([]model.CustomStatus, 0, len(recent))
	for i := range recent {
		if !isMeetingStatus(&recent[i]) {
			kept = append(kept, recent[i])
		}
	}
	if len(kept) == len(recent) {
		return
	}
	data, err := json.Marshal(kept)
	if err != nil {
		return
	}
	pref.Value = string(data)
	if appErr := p.API.UpdatePreferencesForUser(userID, []model.Preference{pref}); appErr != nil {
		p.API.LogWarn("Failed to tidy recent statuses", "user_id", userID, "error", appErr.Error())
	}
}

// clearMeetingStatus puts back the status the user had before, unless they
// have chosen another one in the meantime.
func (p *Plugin) clearMeetingStatus(userID, meetingID string) {
	var saved savedStatus
	if found, _ := p.kvGet(keyCustomStatus+userID, &saved); !found || saved.MeetingID != meetingID {
		return
	}
	defer p.forgetStatus(userID)
	user, appErr := p.API.GetUser(userID)
	if appErr != nil || !isMeetingStatus(user.GetCustomStatus()) {
		return
	}
	previous := saved.Previous
	if previous != nil && (previous.ExpiresAt.IsZero() || previous.ExpiresAt.After(time.Now())) {
		if appErr := p.API.UpdateUserCustomStatus(userID, previous); appErr == nil {
			return
		}
	}
	if appErr := p.API.RemoveUserCustomStatus(userID); appErr != nil {
		p.API.LogWarn("Failed to clear meeting status", "user_id", userID, "error", appErr.Error())
	}
}

func (p *Plugin) forgetStatus(userID string) {
	if appErr := p.API.KVDelete(keyCustomStatus + userID); appErr != nil {
		p.API.LogWarn("Failed to forget saved status", "user_id", userID, "error", appErr.Error())
	}
	if err := p.indexStatus(userID, false); err != nil {
		p.API.LogWarn("Failed to unindex meeting status", "user_id", userID, "error", err.Error())
	}
}

// indexStatus keeps the list of users whose status the plugin has changed.
func (p *Plugin) indexStatus(userID string, add bool) error {
	return p.mutateList(keyStatusIndex, func(ids []string) []string {
		kept := ids[:0]
		for _, id := range ids {
			if id != userID {
				kept = append(kept, id)
			}
		}
		if add {
			kept = append(kept, userID)
		}
		return kept
	})
}

// sweepStatuses puts statuses back when their meeting is over but the end was
// missed, for example because the plugin was disabled during the meeting. The
// status itself never expires, so this is what guarantees it goes away.
func (p *Plugin) sweepStatuses() {
	var ids []string
	if _, err := p.kvGet(keyStatusIndex, &ids); err != nil {
		return
	}
	for _, userID := range ids {
		var saved savedStatus
		found, err := p.kvGet(keyCustomStatus+userID, &saved)
		if err != nil {
			continue
		}
		if !found {
			_ = p.indexStatus(userID, false)
			continue
		}
		m, err := p.getMeeting(saved.MeetingID)
		if err == nil && m.isOpen() && contains(m.Present, userID) {
			continue
		}
		p.clearMeetingStatus(userID, saved.MeetingID)
	}
}
