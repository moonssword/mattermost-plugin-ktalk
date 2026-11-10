package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

const (
	syncInterval      = 30 * time.Second
	calendarLookAhead = 5 // Weeks to look for the next occurrence of a series.
)

// runSync is the scheduled job. Polling is the primary source of truth: it
// works when the space or the network does not allow webhooks, and webhooks
// only make it run sooner.
func (p *Plugin) runSync() {
	if p.client() == nil {
		return
	}
	p.metrics.syncRuns.Add(1)
	meetings, err := p.activeMeetings()
	if err != nil {
		p.API.LogError("Sync: failed to list meetings", "error", err.Error())
		return
	}
	for _, m := range meetings {
		p.syncMeeting(m)
	}
	p.sweepStatuses()
}

func (p *Plugin) syncMeetingByID(id string) {
	m, err := p.getMeeting(id)
	if err != nil {
		p.API.LogWarn("Sync: meeting unavailable", "meeting_id", id, "error", err.Error())
		return
	}
	p.syncMeeting(m)
}

// syncMeeting brings one meeting up to date with Talk.
func (p *Plugin) syncMeeting(m *Meeting) {
	client := p.client()
	if client == nil || m.LinkOnly {
		p.dropActive(m.ID)
		return
	}
	now := time.Now().UTC()

	if m.Status == statusScheduled {
		p.syncSchedule(client, m, now)
		// Rooms are only polled close to the start of the meeting.
		if m.Schedule != nil && now.Before(m.Schedule.Start.Add(-joinOpensBefore)) {
			return
		}
	}

	if m.isOpen() {
		p.syncRoom(client, m, now)
	}

	current, err := p.getMeeting(m.ID)
	if err != nil {
		return
	}
	if current.Status == statusLive || current.Status == statusEnded {
		p.syncArtifacts(client, current, now)
		if current, err = p.getMeeting(m.ID); err != nil {
			return
		}
	}
	if current.Status == statusCancelled || artifactsSettled(current, now) {
		p.finishTracking(current)
	}
}

func (p *Plugin) syncRoom(client *talk.Client, m *Meeting, now time.Time) {
	ctx, cancel := apiContext()
	defer cancel()
	room, err := client.GetRoom(ctx, m.RoomName)
	if talk.IsNotFound(err) {
		p.markEnded(m.ID, now)
		return
	}
	if err != nil {
		p.noteAPIError(err)
		p.API.LogWarn("Sync: room unavailable", "room", m.RoomName, "error", err.Error())
		return
	}

	recording := m.Recording
	if room.UsersOnline > 0 {
		if key, recErr := client.GetActiveRecording(ctx, m.RoomName); recErr == nil {
			recording = key != ""
		} else {
			p.API.LogDebug("Sync: active recording unavailable", "room", m.RoomName, "error", recErr.Error())
		}
	}

	present := []string{}
	if room.UsersOnline > 0 {
		present = p.presentUsers(room)
	}
	var before, ringingBefore []string
	finished := false
	updated, changed, err := p.updateMeeting(m.ID, func(fresh *Meeting) bool {
		if !fresh.isOpen() {
			return false
		}
		ringingBefore = callingUsers(fresh.Callees)
		before = fresh.Present
		finished = applyRoomState(fresh, room, now)
		if fresh.Status == statusLive {
			fresh.Recording = recording
		}
		fresh.Present = present
		if !fresh.isOpen() {
			fresh.Present = nil
		}
		return true
	})
	if err != nil {
		p.API.LogWarn("Sync: update failed", "meeting_id", m.ID, "error", err.Error())
		return
	}
	p.updatePresence(m.ID, before, updated.Present)
	if stopped, _ := diffPresence(callingUsers(updated.Callees), ringingBefore); len(stopped) > 0 {
		// Ringing timed out or the call was answered: silence the clients.
		p.dismissIncoming(m.ID, stopped...)
	}
	if changed {
		p.refreshCard(updated)
	}
	if finished {
		p.collectResults(client, updated)
		if updated.Schedule != nil && updated.Schedule.Recurring {
			p.scheduleNextOccurrence(client, updated, now)
		}
	}
}

func (p *Plugin) markEnded(id string, now time.Time) {
	var before []string
	updated, changed, err := p.updateMeeting(id, func(m *Meeting) bool {
		if !m.isOpen() {
			return false
		}
		before = m.Present
		expireMeeting(m, now)
		m.Present = nil
		return true
	})
	if err == nil && changed {
		p.updatePresence(id, before, nil)
		p.refreshCard(updated)
	}
}

// collectResults fills the finished card. History in Talk appears some minutes
// after a conference ends, so syncArtifacts calls this again until it is there.
func (p *Plugin) collectResults(client *talk.Client, m *Meeting) {
	ctx, cancel := apiContext()
	defer cancel()

	since := m.CreatedAt.Add(-time.Minute)
	if m.StartedAt != nil {
		since = m.StartedAt.Add(-time.Hour)
	}
	conferenceKey := m.ConferenceKey
	if conferenceKey == "" {
		conferences, err := client.ListRoomConferences(ctx, m.RoomName, since)
		if err != nil {
			p.noteAPIError(err)
			p.API.LogWarn("Sync: conference history unavailable", "room", m.RoomName, "error", err.Error())
		}
		conferenceKey = latestConference(conferences)
	}

	var results *MeetingResults
	if conferenceKey != "" {
		if err := p.kvSet(keyConf+conferenceKey, m.ID, finishedKeepFor); err != nil {
			p.API.LogWarn("Sync: conference index failed", "error", err.Error())
		}
		results = &MeetingResults{URL: p.getConfig().artifactsURL(conferenceKey)}
		details, err := client.GetConference(ctx, conferenceKey)
		if err != nil {
			p.noteAPIError(err)
			p.API.LogWarn("Sync: conference details unavailable", "conference", conferenceKey, "error", err.Error())
		} else if details.Artifacts != nil {
			fillResults(results, details.Artifacts)
		}
	}

	if conferenceKey == "" {
		return
	}
	updated, changed, err := p.updateMeeting(m.ID, func(fresh *Meeting) bool {
		fresh.ConferenceKey = conferenceKey
		fresh.Results = results
		return true
	})
	if err != nil {
		p.API.LogWarn("Sync: failed to save results", "meeting_id", m.ID, "error", err.Error())
		return
	}
	if changed {
		p.refreshCard(updated)
	}
}

func latestConference(conferences []talk.ConferenceInfo) string {
	sort.Slice(conferences, func(i, j int) bool { return conferences[i].StartTime.After(conferences[j].StartTime) })
	if len(conferences) == 0 {
		return ""
	}
	return conferences[0].Key
}

func fillResults(results *MeetingResults, artifacts *talk.ConferenceArtifacts) {
	for i := range artifacts.Participants {
		if name := artifacts.Participants[i].Name(); name != "" {
			results.Attended = append(results.Attended, name)
		}
	}
	for i := range artifacts.InvitedParticipants {
		if name := artifacts.InvitedParticipants[i].Name(); name != "" {
			results.Missed = append(results.Missed, name)
		}
	}
	results.Polls = len(artifacts.Polls)
	results.Notes = len(artifacts.Notes)
	results.Boards = len(artifacts.Whiteboards)
}

// syncArtifacts posts new recordings and their summaries to the thread. Each
// item is claimed with compare-and-set first, so only one node posts it.
func (p *Plugin) syncArtifacts(client *talk.Client, m *Meeting, now time.Time) {
	if now.Sub(m.ArtifactPollAt) < artifactInterval {
		return
	}
	claimed := false
	_, _, err := p.updateMeeting(m.ID, func(fresh *Meeting) bool {
		if now.Sub(fresh.ArtifactPollAt) < artifactInterval {
			return false
		}
		fresh.ArtifactPollAt = now
		claimed = true
		return true
	})
	if err != nil || !claimed {
		return
	}

	if needsResults(m) {
		p.collectResults(client, m)
	}

	ctx, cancel := apiContext()
	defer cancel()
	since := m.CreatedAt.Add(-time.Minute)
	page, err := client.ListRecordings(ctx, talk.RecordingFilter{Query: m.RoomName, StartFrom: &since, Top: 20})
	if err != nil {
		p.noteAPIError(err)
		p.API.LogDebug("Sync: recordings unavailable", "room", m.RoomName, "error", err.Error())
		return
	}
	for i := range page.Entities {
		if page.Entities[i].RoomName == m.RoomName && recordingReady(m, &page.Entities[i], now) {
			p.announceRecording(client, m.ID, &page.Entities[i])
		}
	}

	current, err := p.getMeeting(m.ID)
	if err != nil {
		return
	}
	for i := range current.Recordings {
		if !current.Recordings[i].SummaryDone {
			p.announceSummary(client, current.ID, current.Recordings[i].Key)
		}
	}
}

// announceRecording registers a recording and posts it once.
func (p *Plugin) announceRecording(client *talk.Client, meetingID string, rec *talk.Recording) {
	cfg := p.getConfig()
	isNew := false
	m, _, err := p.updateMeeting(meetingID, func(fresh *Meeting) bool {
		for i := range fresh.Recordings {
			if fresh.Recordings[i].Key == rec.Key {
				return false
			}
		}
		isNew = true
		fresh.Recordings = append(fresh.Recordings, MeetingRecording{
			Key: rec.Key, Title: rec.Title, URL: cfg.recordingURL(rec.Key), Duration: rec.Duration, Posted: true,
		})
		return true
	})
	if err != nil || !isNew {
		return
	}
	if err := p.kvSet(keyRec+rec.Key, meetingID, finishedKeepFor); err != nil {
		p.API.LogWarn("Recording index failed", "error", err.Error())
	}
	p.refreshCard(m)

	// A recording started from Mattermost gets the access chosen then: Talk
	// accepts it only now that the recording is finished. An automatic
	// recording takes the administrator's default. A recording started in
	// Talk keeps what its author chose there.
	choice, chosen := m.RecordingChoices[rec.Key]
	switch {
	case chosen:
		p.applyRecordingChoice(client, rec.Key, choice)
	case !chosen && m.Schedule != nil && m.Schedule.AutoRecord:
		ctx, cancel := apiContext()
		if err := client.SetRecordingLinkAccess(ctx, rec.Key, linkScope(cfg.defaultRecordingAccess())); err != nil {
			p.noteAPIError(err)
			p.API.LogWarn("Failed to set recording access", "recording", rec.Key, "error", err.Error())
		}
		cancel()
	}
	if cfg.PostRecordings {
		message := fmt.Sprintf("🎬 Запись встречи готова: [%s](%s)", safeInline(rec.Title), cfg.recordingURL(rec.Key))
		if rec.Duration > 0 {
			message += " · " + formatDuration(rec.Duration)
		}
		p.botReply(m, message, postTypeRec, map[string]any{"meeting_id": m.ID, "recording_key": rec.Key, "recording_url": cfg.recordingURL(rec.Key)})
	}
	if p.userPref(m.CreatorID, prefNotifyRec, "false") == "true" {
		p.dmUser(m.CreatorID, fmt.Sprintf("Запись встречи **%s** готова: %s", safeInline(m.Title), cfg.recordingURL(rec.Key)))
	}
}

// announceSummary posts the short summary once it is ready.
func (p *Plugin) announceSummary(client *talk.Client, meetingID, key string) {
	cfg := p.getConfig()
	ctx, cancel := apiContext()
	defer cancel()
	artifacts, err := client.GetSpeechArtifacts(ctx, key)
	if err != nil {
		p.API.LogDebug("Summary unavailable", "recording", key, "error", err.Error())
		return
	}
	summary := artifacts.ShortSummary
	if summary == nil || !summaryIsFinal(summary.Status) {
		return
	}

	claimed := false
	m, _, err := p.updateMeeting(meetingID, func(fresh *Meeting) bool {
		for i := range fresh.Recordings {
			if fresh.Recordings[i].Key == key && !fresh.Recordings[i].SummaryDone {
				fresh.Recordings[i].SummaryDone = true
				claimed = true
				return true
			}
		}
		return false
	})
	if err != nil || !claimed {
		return
	}
	if summary.Status != talk.SpeechSuccess || summary.Text() == "" || !cfg.PostSummaries {
		return
	}
	// The thread is read by the whole channel, so it gets a notice only. The
	// text opens through the plugin, which asks Talk who may read it.
	message := "📝 **Краткий пересказ встречи готов.** Открыть его могут автор записи и участники встречи."
	p.botReply(m, message, postTypeRec, map[string]any{"meeting_id": m.ID, "recording_key": key, "recording_url": cfg.recordingURL(key), "is_summary": true})
}

func formatDuration(seconds int) string {
	if seconds >= 3600 {
		return fmt.Sprintf("%d ч %d мин", seconds/3600, seconds%3600/60)
	}
	if seconds >= 60 {
		return fmt.Sprintf("%d мин", seconds/60)
	}
	return fmt.Sprintf("%d с", seconds)
}

func (p *Plugin) dropActive(id string) {
	if err := p.removeActive(id); err != nil {
		p.API.LogWarn("Failed to drop active meeting", "meeting_id", id, "error", err.Error())
	}
}

// finishTracking stops polling a meeting and lets its record expire.
func (p *Plugin) finishTracking(m *Meeting) {
	p.dropActive(m.ID)
	if err := p.kvSet(keyMeeting+m.ID, m, finishedKeepFor); err != nil {
		p.API.LogWarn("Failed to set meeting expiry", "meeting_id", m.ID, "error", err.Error())
	}
}

// --- Webhook driven sync ---

// syncSoon runs one sync per meeting after a short pause, so a burst of join
// events causes a single API call. Per node, which is enough for a debounce.
func (p *Plugin) syncSoon(meetingID string) {
	p.debounceLock.Lock()
	defer p.debounceLock.Unlock()
	if p.debounced == nil {
		return
	}
	if timer, exists := p.debounced[meetingID]; exists {
		timer.Reset(2 * time.Second)
		return
	}
	p.debounced[meetingID] = time.AfterFunc(2*time.Second, func() {
		p.debounceLock.Lock()
		delete(p.debounced, meetingID)
		p.debounceLock.Unlock()
		p.syncMeetingByID(meetingID)
	})
}

// --- Visibility of recordings ---

// Talk decides who may see a recording: its author and the meeting's
// participants hold roles on it, and its owner may share it further. The
// plugin reads those grants and nothing else. Channel membership is not a
// source: a channel may gain members who never took part, and Mattermost lets
// system admins read every channel.

// recordingViewer is the Talk user a Mattermost user reads recordings as. A
// user without a verified address matched in Talk sees no recordings.
func (p *Plugin) recordingViewer(user *model.User) (string, error) {
	if problem := p.emailProblem(user); problem != nil {
		return "", problem
	}
	identity, ok := p.talkUser(user)
	if !ok || identity.Key == "" {
		return "", newUserError(http.StatusForbidden, "Записи доступны только пользователям Толка: ваш адрес почты не найден в пространстве.")
	}
	return identity.Key, nil
}

// accessCacheTTL is how long the grants of a recording are remembered. The
// list is reloaded on every visit to the tab, and each recording costs one
// request to Talk, so a short memory keeps that from multiplying.
const accessCacheTTL = time.Minute

// recordingAccess returns the grants of a recording, from the cache when
// they were read within accessCacheTTL.
func (p *Plugin) recordingAccess(ctx context.Context, client *talk.Client, recordingKey string) (*talk.RecordingAccess, error) {
	var cached talk.RecordingAccess
	if found, err := p.kvGet(keyRecAccess+recordingKey, &cached); err == nil && found {
		return &cached, nil
	}
	access, err := client.GetRecordingAccess(ctx, recordingKey)
	if err != nil {
		return nil, err
	}
	if err := p.kvSet(keyRecAccess+recordingKey, access, accessCacheTTL); err != nil {
		p.API.LogDebug("Recording access cache write failed", "error", err.Error())
	}
	return access, nil
}

// canAccessRecording asks Talk whether a Talk user holds a role on a recording.
// Any doubt, an API error included, means no.
func (p *Plugin) canAccessRecording(ctx context.Context, client *talk.Client, talkKey, recordingKey string) bool {
	access, err := p.recordingAccess(ctx, client, recordingKey)
	if err != nil {
		p.noteAPIError(err)
		p.API.LogWarn("Recording access unavailable", "recording", recordingKey, "error", err.Error())
		return false
	}
	return access.Grants(talkKey)
}

// accessibleRecordings keeps the recordings a Talk user may see, in order.
// Grants are read in parallel, a few at a time.
func (p *Plugin) accessibleRecordings(ctx context.Context, client *talk.Client, talkKey string, recs []talk.Recording) []talk.Recording {
	const workers = 8
	allowed := make([]bool, len(recs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(workers, len(recs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				allowed[i] = p.canAccessRecording(ctx, client, talkKey, recs[i].Key)
			}
		}()
	}
	for i := range recs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	out := make([]talk.Recording, 0, len(recs))
	for i := range recs {
		if allowed[i] {
			out = append(out, recs[i])
		}
	}
	return out
}

func (p *Plugin) recordingForUser(user *model.User, key string) (*talk.Recording, error) {
	client, err := p.requireClient()
	if err != nil {
		return nil, err
	}
	talkKey, err := p.recordingViewer(user)
	if err != nil {
		return nil, err
	}
	ctx, cancel := apiContext()
	defer cancel()
	rec, err := client.GetRecording(ctx, key)
	if talk.IsNotFound(err) {
		return nil, newUserError(http.StatusNotFound, "Запись не найдена.")
	}
	if err != nil {
		return nil, p.describeAPIError("get recording", err)
	}
	if !p.canAccessRecording(ctx, client, talkKey, key) {
		return nil, errForbidden
	}
	return rec, nil
}

var errSeriesDone = errors.New("no next occurrence")

func sortTranscript(lines []transcriptLine) {
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Start < lines[j].Start })
}
