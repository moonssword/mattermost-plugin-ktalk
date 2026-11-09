package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/mail"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

const maxRepeatCount = 100

// scheduleInput is the validated content of the scheduling dialog.
type scheduleInput struct {
	Title          string
	Description    string
	Start          time.Time
	DurationMin    int
	Repeat         string
	RepeatCount    int
	UserIDs        []string
	ExternalEmails []string
	Pin            string
	Lobby          bool
	Guests         bool
	AutoRecord     bool
	SIP            bool
}

// parseScheduleSubmission validates dialog fields, returning per-field errors.
func parseScheduleSubmission(sub map[string]any, now time.Time) (scheduleInput, map[string]string) {
	in := scheduleInput{}
	errs := map[string]string{}

	in.Title = clip(stringField(sub, "title"), maxTitleLength)
	if in.Title == "" {
		errs["title"] = "Укажите тему."
	}
	in.Description = strings.TrimSpace(stringField(sub, "description"))

	start, err := time.Parse(time.RFC3339, stringField(sub, "start"))
	switch {
	case err != nil:
		errs["start"] = "Укажите дату и время."
	case start.Before(now.Add(-time.Minute)):
		errs["start"] = "Время начала уже прошло."
	default:
		in.Start = start.UTC()
	}

	in.DurationMin, err = strconv.Atoi(stringField(sub, "duration"))
	if err != nil || in.DurationMin <= 0 {
		errs["duration"] = "Выберите длительность."
	}

	in.Repeat = stringField(sub, "repeat")
	switch in.Repeat {
	case "", "none":
		in.Repeat = ""
	case "daily", "weekly", "monthly":
		count, ok := intField(sub, "repeat_count")
		in.RepeatCount = count
		if !ok || count < 2 || count > maxRepeatCount {
			errs["repeat_count"] = fmt.Sprintf("Укажите число встреч от 2 до %d.", maxRepeatCount)
		}
	default:
		errs["repeat"] = "Неизвестный вариант."
	}

	in.UserIDs = listField(sub, "participants")
	for _, raw := range strings.FieldsFunc(stringField(sub, "external"), func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' }) {
		address, parseErr := mail.ParseAddress(raw)
		if parseErr != nil {
			errs["external"] = "Неверный адрес: " + raw
			break
		}
		in.ExternalEmails = append(in.ExternalEmails, address.Address)
	}

	in.Pin = strings.TrimSpace(stringField(sub, "pin"))
	if in.Pin != "" && !validPin(in.Pin) {
		errs["pin"] = "ПИН-код: от 4 до 6 цифр."
	}
	in.Lobby = boolField(sub, "lobby")
	in.Guests = boolField(sub, "guests")
	in.AutoRecord = boolField(sub, "auto_record")
	in.SIP = boolField(sub, "sip")
	return in, errs
}

func stringField(sub map[string]any, key string) string {
	value, _ := sub[key].(string)
	return value
}

// intField reads a whole number. A text field with the number subtype comes
// as a string from older clients and as a JSON number from Mattermost 11.
func intField(sub map[string]any, key string) (int, bool) {
	switch value := sub[key].(type) {
	case float64:
		if value != math.Trunc(value) {
			return 0, false
		}
		return int(value), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(value))
		return n, err == nil
	}
	return 0, false
}

func boolField(sub map[string]any, key string) bool {
	switch value := sub[key].(type) {
	case bool:
		return value
	case string:
		return value == "true"
	}
	return false
}

// listField reads a multiselect, which arrives as a list or a joined string.
func listField(sub map[string]any, key string) []string {
	switch value := sub[key].(type) {
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if value == "" {
			return nil
		}
		return strings.Split(value, ",")
	}
	return nil
}

// buildRecurrence converts the dialog choice to the API pattern.
func buildRecurrence(repeat string, count int, start time.Time) *talk.Recurrence {
	if repeat == "" {
		return nil
	}
	one := 1
	rec := &talk.Recurrence{Type: repeat, StartDate: &start, NumberOfEvents: &count, Interval: &one}
	switch repeat {
	case "weekly":
		rec.DaysOfTheWeek = []string{strings.ToLower(start.Weekday().String())}
	case "monthly":
		day := start.Day()
		rec.DayOfMonth = &day
	}
	return rec
}

// calendarClient picks the key to call the calendar with. A personal key acts
// as the user; otherwise the space key acts on the user's verified mailbox.
func (p *Plugin) calendarClient(userID string) (*talk.Client, bool, error) {
	if personal := p.personalClient(userID); personal != nil {
		return personal, true, nil
	}
	if p.getConfig().CalendarPersonalOnly {
		return nil, false, errPersonalKeyRequired
	}
	client, err := p.requireClient()
	return client, false, err
}

var errPersonalKeyRequired = newCodedError(http.StatusConflict, codePersonalKey, "Для работы с календарём подключите личный API-ключ Толка командой /talk connect.")

func (p *Plugin) calendarClientFor(m *Meeting) (*talk.Client, error) {
	if m.Schedule != nil && m.Schedule.UsePersonalKey {
		if personal := p.personalClient(m.CreatorID); personal != nil {
			return personal, nil
		}
	}
	return p.requireClient()
}

func userLocation(user *model.User) *time.Location {
	name := user.GetPreferredTimezone()
	if name == "" {
		return time.UTC
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return location
}

// resolveAttendees splits invited users into Talk keys and external addresses.
func (p *Plugin) resolveAttendees(userIDs []string) (keys, emails []string, err error) {
	if len(userIDs) == 0 {
		return nil, nil, nil
	}
	users, appErr := p.API.GetUsersByIds(userIDs)
	if appErr != nil {
		return nil, nil, appErr
	}
	identities, err := p.resolveTalkUsers(users)
	if err != nil {
		return nil, nil, err
	}
	for _, user := range users {
		if identity, ok := identities[user.Id]; ok {
			keys = append(keys, identity.Key)
		} else if user.Email != "" && !user.IsBot {
			emails = append(emails, user.Email) // Joins as an external attendee.
		}
	}
	return keys, emails, nil
}

func eventFromInput(in scheduleInput, roomName, timezone string, keys, emails []string) talk.CalendarEvent {
	external := true
	event := talk.CalendarEvent{
		Start:                  in.Start,
		End:                    in.Start.Add(time.Duration(in.DurationMin) * time.Minute),
		Timezone:               timezone,
		Subject:                plainTitle(in.Title),
		Description:            in.Description,
		RoomName:               roomName,
		AllowAnonymous:         in.Guests,
		EnableSIP:              in.SIP,
		PinCode:                in.Pin,
		EnableAutoRecording:    in.AutoRecord,
		RequiredUserKeys:       keys,
		RequiredExternalEmails: append(emails, in.ExternalEmails...),
		ControlledExternally:   &external,
	}
	if in.Repeat != "" {
		event.IsRecurring = true
		event.Recurrence = buildRecurrence(in.Repeat, in.RepeatCount, in.Start)
	}
	return event
}

// scheduleMeeting creates the room, the calendar event and the card.
func (p *Plugin) scheduleMeeting(user *model.User, channel *model.Channel, rootID string, in scheduleInput) (*Meeting, error) {
	if err := p.checkCanStart(user, channel.Id); err != nil {
		return nil, err
	}
	if problem := p.emailProblem(user); problem != nil {
		return nil, problem
	}
	email := p.talkEmail(user)
	organizer, ok := p.talkUser(user)
	if !ok {
		return nil, notInTalkError()
	}
	client, personal, err := p.calendarClient(user.Id)
	if err != nil {
		return nil, err
	}
	keys, emails, err := p.resolveAttendees(in.UserIDs)
	if err != nil {
		return nil, p.describeAPIError("resolve attendees", err)
	}

	cfg := p.getConfig()
	now := time.Now().UTC()
	roomName := p.newRoomName(user.Id, channel)
	timezone := timezoneLabel(in.Start.In(userLocation(user)))
	end := in.Start.Add(time.Duration(in.DurationMin) * time.Minute)

	m := &Meeting{
		ID: model.NewId(), RoomName: roomName, URL: cfg.roomURL(roomName), Title: in.Title,
		ChannelID: channel.Id, RootID: rootID, CreatorID: user.Id, CreatorKey: organizer.Key,
		CreatorName: user.GetDisplayName(model.ShowFullName), Kind: kindScheduled, Status: statusScheduled,
		CreatedAt: now, Lobby: in.Lobby, Pin: in.Pin, Moderators: []string{organizer.Key},
		Schedule: &Schedule{
			Email: email, UsePersonalKey: personal, Start: in.Start, End: end, Timezone: timezone,
			Description: in.Description, Repeat: in.Repeat, RepeatCount: in.RepeatCount, Recurring: in.Repeat != "",
			UserIDs: in.UserIDs, UserKeys: keys, ExternalEmails: in.ExternalEmails, AllowGuests: in.Guests,
			AutoRecord: in.AutoRecord, EnableSIP: in.SIP,
		},
	}
	m.GuestsUntil = scheduledGuests(in, end)

	ctx, cancel := apiContext()
	defer cancel()
	// Personal keys cannot be assumed to manage rooms, so the space key does.
	roomClient, err := p.requireClient()
	if err != nil {
		return nil, err
	}
	if _, err := roomClient.PutRoom(ctx, roomName, roomParamsFor(m)); err != nil {
		return nil, p.describeAPIError("put room", err)
	}
	item, err := client.CreateEvent(ctx, email, eventFromInput(in, roomName, timezone, keys, emails))
	if err != nil {
		return nil, p.calendarError(client, user.Id, "create event", err)
	}
	m.Schedule.EventID = item.ID
	if item.OnlineMeetingURL != "" {
		m.URL = item.OnlineMeetingURL
	}
	m.Schedule.Attendees = attendeesFrom(item)
	m.Schedule.AttendeesAt = now
	m.Schedule.InviteeKeys = p.inviteeKeys(in.UserIDs)
	p.absorbRoom(roomClient, m)

	if err := p.publishMeeting(m); err != nil {
		return nil, err
	}
	p.metrics.meetingsCreated.Add(1)
	p.audit("scheduleMeeting", user.Id, m.ID)
	return m, nil
}

// scheduledGuests is how long guests may enter a scheduled meeting: until two
// hours after it ends, or for a year for a series. Nil closes the room to them.
func scheduledGuests(in scheduleInput, end time.Time) *time.Time {
	if !in.Guests {
		return nil
	}
	until := end.Add(2 * time.Hour)
	if in.Repeat != "" {
		until = until.AddDate(1, 0, 0)
	}
	return &until
}

// rescheduleMeeting applies dialog changes to an existing event.
func (p *Plugin) rescheduleMeeting(user *model.User, m *Meeting, in scheduleInput) error {
	if m.Schedule == nil {
		return newUserError(http.StatusBadRequest, "Это не запланированная встреча.")
	}
	client, err := p.calendarClientFor(m)
	if err != nil {
		return err
	}
	keys, emails, err := p.resolveAttendees(in.UserIDs)
	if err != nil {
		return p.describeAPIError("resolve attendees", err)
	}
	timezone := timezoneLabel(in.Start.In(userLocation(user)))
	event := eventFromInput(in, m.RoomName, timezone, nil, nil)

	ctx, cancel := apiContext()
	defer cancel()
	item, err := client.UpdateEvent(ctx, m.Schedule.Email, m.Schedule.EventID, event)
	if err != nil {
		return p.calendarError(client, m.CreatorID, "update event", err)
	}
	// Talk refuses an attendee list without the organizer.
	if m.CreatorKey != "" && !contains(keys, m.CreatorKey) {
		keys = append([]string{m.CreatorKey}, keys...)
	}
	if updated, err := client.SetAttendees(ctx, m.Schedule.Email, m.Schedule.EventID, keys, append(emails, in.ExternalEmails...)); err == nil {
		item = updated
	} else {
		p.noteAPIError(err)
		p.API.LogWarn("Failed to update attendees", "meeting_id", m.ID, "error", err.Error())
	}
	// The waiting room is a room setting, not a calendar one.
	roomClient, err := p.requireClient()
	if err != nil {
		return err
	}
	// Room settings are sent from the meeting, so it takes the new values first.
	guests := scheduledGuests(in, event.End)
	m.Title, m.Lobby, m.Pin, m.GuestsUntil = in.Title, in.Lobby, in.Pin, guests
	m.Schedule.AutoRecord = in.AutoRecord
	if _, err := roomClient.PutRoom(ctx, m.RoomName, roomParamsFor(m)); err != nil {
		p.noteAPIError(err)
		p.API.LogWarn("Failed to update room after reschedule", "meeting_id", m.ID, "error", err.Error())
	}
	p.absorbRoom(roomClient, m)
	sip := m.SIP
	invitees := p.inviteeKeys(in.UserIDs)
	return p.saveAndRefresh(m.ID, func(fresh *Meeting) {
		fresh.Title = in.Title
		fresh.Lobby = in.Lobby
		fresh.Pin = in.Pin
		fresh.SIP = sip
		fresh.GuestsUntil = guests
		sc := fresh.Schedule
		sc.Start, sc.End, sc.Timezone, sc.Description = in.Start, event.End, timezone, in.Description
		sc.Repeat, sc.RepeatCount, sc.Recurring = in.Repeat, in.RepeatCount, in.Repeat != ""
		sc.UserIDs, sc.UserKeys, sc.ExternalEmails = in.UserIDs, keys, in.ExternalEmails
		sc.AllowGuests, sc.AutoRecord, sc.EnableSIP, sc.Reminded = in.Guests, in.AutoRecord, in.SIP, false
		sc.Attendees = attendeesFrom(item)
		sc.AttendeesAt = time.Now().UTC()
		sc.InviteeKeys = invitees
	})
}

// cancelSchedule cancels the calendar event and closes the card.
func (p *Plugin) cancelSchedule(userID, meetingID string) error {
	m, err := p.controlledMeeting(userID, meetingID)
	if err != nil {
		return err
	}
	if m.Schedule == nil {
		return newUserError(http.StatusBadRequest, "Это не запланированная встреча.")
	}
	client, err := p.calendarClientFor(m)
	if err != nil {
		return err
	}
	ctx, cancel := apiContext()
	defer cancel()
	if err := client.CancelEvent(ctx, m.Schedule.Email, m.Schedule.EventID, ""); err != nil && !talk.IsNotFound(err) {
		return p.calendarError(client, m.CreatorID, "cancel event", err)
	}
	p.audit("cancelSchedule", userID, m.ID)
	if err := p.saveAndRefresh(m.ID, func(fresh *Meeting) { fresh.Status = statusCancelled }); err != nil {
		return err
	}
	p.dropActive(m.ID)
	return nil
}

// absorbRoom copies dial-in details and the PIN from the room in Talk, which the
// calendar may have changed, into the meeting.
func (p *Plugin) absorbRoom(client *talk.Client, m *Meeting) {
	ctx, cancel := apiContext()
	defer cancel()
	room, err := client.GetRoom(ctx, m.RoomName)
	if err != nil {
		p.API.LogDebug("Room details unavailable", "room", m.RoomName, "error", err.Error())
		return
	}
	if room.SIPSettings != nil && (room.SIPSettings.SIPNumber != "" || len(room.SIPSettings.PhoneNumbers) > 0) {
		m.SIP = room.SIPSettings
	} else {
		m.SIP = nil
	}
	if room.SecurityType == "pinCode" && room.PinCode != "" {
		m.Pin = room.PinCode
	} else if room.SecurityType == "none" {
		m.Pin = ""
	}
}

func attendeesFrom(item *talk.CalendarItem) []MeetingAttendee {
	attendees := make([]MeetingAttendee, 0, len(item.RequiredAttendees)+len(item.OptionalAttendees))
	for _, list := range [][]talk.Attendee{item.RequiredAttendees, item.OptionalAttendees} {
		for i := range list {
			name := strings.TrimSpace(list[i].Name)
			if name == "" && list[i].User != nil {
				name = list[i].User.DisplayName()
			}
			if name == "" {
				name = list[i].Mailbox
			}
			attendee := MeetingAttendee{Name: name, Status: list[i].Status}
			if list[i].User != nil {
				attendee.Key = list[i].User.Key
			}
			attendees = append(attendees, attendee)
		}
	}
	return attendees
}

// syncSchedule refreshes responses and sends the reminder.
func (p *Plugin) syncSchedule(_ *talk.Client, m *Meeting, now time.Time) {
	sc := m.Schedule
	if sc == nil {
		return
	}
	cfg := p.getConfig()
	remindAt := sc.Start.Add(-time.Duration(cfg.ReminderMinutes) * time.Minute)
	if !sc.Reminded && now.After(remindAt) && now.Before(sc.Start) {
		claimed := false
		updated, _, err := p.updateMeeting(m.ID, func(fresh *Meeting) bool {
			if fresh.Schedule == nil || fresh.Schedule.Reminded {
				return false
			}
			fresh.Schedule.Reminded = true
			claimed = true
			return true
		})
		if err == nil && claimed {
			p.sendReminder(updated)
		}
	}

	if now.Sub(sc.AttendeesAt) < attendeeInterval {
		return
	}
	client, err := p.calendarClientFor(m)
	if err != nil {
		return
	}
	ctx, cancel := apiContext()
	defer cancel()
	item, err := p.calendarItem(ctx, client, m)
	if err != nil {
		if client.Personal() && talk.IsStatus(err, http.StatusUnauthorized) {
			_ = p.calendarError(client, m.CreatorID, "refresh attendees", err)
			return
		}
		p.noteAPIError(err)
		p.API.LogDebug("Calendar refresh failed", "meeting_id", m.ID, "error", err.Error())
		return
	}
	if item != nil {
		invitees := p.inviteeKeys(sc.UserIDs)
		updated, changed, err := p.updateMeeting(m.ID, func(fresh *Meeting) bool {
			if fresh.Schedule == nil {
				return false
			}
			fresh.Schedule.InviteeKeys = invitees
			fresh.Schedule.Attendees = attendeesFrom(item)
			fresh.Schedule.AttendeesAt = now
			if item.IsCancelled {
				fresh.Status = statusCancelled
			}
			return true
		})
		if err == nil && changed {
			p.refreshCard(updated)
		}
	}
}

// calendarItem finds a scheduled meeting in its organizer's calendar, or
// returns nil. A recurring meeting is read as a series: the card stands for
// the whole series, and answers given from Mattermost go to the series too.
func (p *Plugin) calendarItem(ctx context.Context, client *talk.Client, m *Meeting) (*talk.CalendarItem, error) {
	sc := m.Schedule
	if sc.Recurring && sc.EventID != "" {
		item, err := client.GetSeries(ctx, sc.Email, sc.EventID)
		if err == nil || !talk.IsNotFound(err) {
			return item, err
		}
	}
	items, err := client.ListEvents(ctx, sc.Email, sc.Start.Add(-time.Minute), sc.End.Add(time.Minute))
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].ID == sc.EventID || items[i].RoomName == m.RoomName {
			return &items[i], nil
		}
	}
	return nil, nil
}

func (p *Plugin) sendReminder(m *Meeting) {
	mentions := []string{}
	if users, appErr := p.API.GetUsersByIds(m.Schedule.UserIDs); appErr == nil {
		for _, user := range users {
			mentions = append(mentions, "@"+user.Username)
		}
	}
	minutes := int(time.Until(m.Schedule.Start).Round(time.Minute).Minutes())
	message := fmt.Sprintf("⏰ Через %d мин начнётся встреча **%s**. [Присоединиться](%s)", minutes, safeInline(plainTitle(m.Title)), m.URL)
	if len(mentions) > 0 {
		message += "\n" + strings.Join(mentions, " ")
	}
	p.botReply(m, message, "", nil)
}

// scheduleNextOccurrence returns a finished series card to the scheduled state
// when the calendar has another occurrence.
func (p *Plugin) scheduleNextOccurrence(_ *talk.Client, m *Meeting, now time.Time) {
	client, err := p.calendarClientFor(m)
	if err != nil {
		return
	}
	next, err := p.findNextOccurrence(client, m, now)
	if err != nil {
		if !errors.Is(err, errSeriesDone) {
			p.noteAPIError(err)
			p.API.LogWarn("Series lookup failed", "meeting_id", m.ID, "error", err.Error())
		}
		return
	}
	if err := p.saveAndRefresh(m.ID, func(fresh *Meeting) {
		fresh.Status = statusScheduled
		fresh.StartedAt, fresh.EndedAt = nil, nil
		fresh.ConferenceKey = ""
		fresh.EmptyPolls = 0
		fresh.Schedule.Start, fresh.Schedule.End = next.Start, next.End
		fresh.Schedule.Reminded = false
	}); err != nil {
		p.API.LogWarn("Failed to resume series", "meeting_id", m.ID, "error", err.Error())
	}
}

func (p *Plugin) findNextOccurrence(client *talk.Client, m *Meeting, now time.Time) (*talk.CalendarItem, error) {
	ctx, cancel := apiContext()
	defer cancel()
	from := now
	for range calendarLookAhead {
		to := from.Add(talk.MaxCalendarPeriod)
		items, err := client.ListEvents(ctx, m.Schedule.Email, from, to)
		if err != nil {
			return nil, err
		}
		for i := range items {
			if items[i].RoomName == m.RoomName && !items[i].IsCancelled && items[i].Start.After(now) {
				return &items[i], nil
			}
		}
		from = to
	}
	return nil, errSeriesDone
}

func notInTalkError() *userError {
	return newCodedError(http.StatusBadRequest, codeNotInTalk,
		"В Контур.Толке нет пользователя с адресом из вашего профиля Mattermost. Адреса в обеих системах должны совпадать.")
}

func isWebURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

// upcomingEvent is a calendar item for the sidebar.
type upcomingEvent struct {
	ID        string    `json:"id"`
	Subject   string    `json:"subject"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	URL       string    `json:"url"`
	Online    int       `json:"online"`
	External  string    `json:"external,omitempty"`
	Recurring bool      `json:"recurring,omitempty"`
	// Repeat is daily, weekly or monthly when known, for a recurring event.
	Repeat string `json:"repeat,omitempty"`
	// Set when the user may change the meeting from Mattermost.
	MeetingID string `json:"meeting_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`

	series string
}

// upcomingForUser lists a user's meetings for the next seven days.
func (p *Plugin) upcomingForUser(user *model.User) ([]upcomingEvent, error) {
	if problem := p.emailProblem(user); problem != nil {
		return nil, problem
	}
	email := p.talkEmail(user)
	if _, ok := p.talkUser(user); !ok {
		return nil, notInTalkError()
	}
	client, _, err := p.calendarClient(user.Id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	ctx, cancel := apiContext()
	defer cancel()
	items, err := client.ListEvents(ctx, email, now.Add(-time.Hour), now.Add(talk.MaxCalendarPeriod-time.Hour))
	if talk.IsNotFound(err) {
		return []upcomingEvent{}, nil
	}
	if err != nil {
		return nil, p.calendarError(client, user.Id, "list events", err)
	}
	cfg := p.getConfig()
	events := make([]upcomingEvent, 0, len(items))
	for i := range items {
		item := &items[i]
		if item.IsCancelled || item.End.Before(now) {
			continue
		}
		event := upcomingEvent{ID: item.ID, Subject: item.Subject, Start: item.Start, End: item.End, URL: item.OnlineMeetingURL, Online: item.OnlineUsersCount, Recurring: item.IsRecurring}
		if event.URL == "" && item.RoomName != "" {
			event.URL = cfg.roomURL(item.RoomName)
		}
		if item.ExternalMeeting != nil {
			event.External = item.ExternalMeeting.Type
			if event.URL == "" {
				event.URL = item.ExternalMeeting.URL
			}
		}
		// This address can come from an outside invitation.
		if !isWebURL(event.URL) {
			event.URL = ""
		}
		p.attachEditable(&event, item.RoomName, user.Id)
		if event.Recurring {
			event.series = seriesKey(item)
			event.Repeat = p.plannedRepeat(item.RoomName)
		}
		events = append(events, event)
	}
	return collapseSeries(events), nil
}

// seriesKey tells occurrences of one recurring meeting apart from another's.
// Occurrences share a Talk room; meetings from other systems share a subject
// and a link.
func seriesKey(item *talk.CalendarItem) string {
	if item.RoomName != "" {
		return "room:" + item.RoomName
	}
	link := item.OnlineMeetingURL
	if item.ExternalMeeting != nil && link == "" {
		link = item.ExternalMeeting.URL
	}
	return "subject:" + item.Subject + "|" + link
}

// plannedRepeat returns how often a meeting planned from Mattermost repeats.
func (p *Plugin) plannedRepeat(roomName string) string {
	if roomName == "" {
		return ""
	}
	m, err := p.getMeetingByIndex(keyRoom + roomName)
	if err != nil || m.Schedule == nil {
		return ""
	}
	return m.Schedule.Repeat
}

// collapseSeries keeps the nearest occurrence of each recurring meeting, so a
// daily meeting takes one line instead of five. When the repeat is unknown, a
// steady one-day step between occurrences reads as daily.
func collapseSeries(events []upcomingEvent) []upcomingEvent {
	slices.SortStableFunc(events, func(a, b upcomingEvent) int { return a.Start.Compare(b.Start) })
	first := map[string]int{}
	steps := map[string][]time.Duration{}
	last := map[string]time.Time{}
	out := make([]upcomingEvent, 0, len(events))
	for _, event := range events {
		if event.series == "" {
			out = append(out, event)
			continue
		}
		if _, seen := first[event.series]; seen {
			steps[event.series] = append(steps[event.series], event.Start.Sub(last[event.series]))
			last[event.series] = event.Start
			continue
		}
		first[event.series] = len(out)
		last[event.series] = event.Start
		out = append(out, event)
	}
	for key, at := range first {
		if out[at].Repeat != "" || len(steps[key]) == 0 {
			continue
		}
		daily := true
		for _, step := range steps[key] {
			daily = daily && step == 24*time.Hour
		}
		if daily {
			out[at].Repeat = "daily"
		}
	}
	return out
}

// attachEditable links a calendar event to the scheduled meeting this plugin
// created for it, when the user may change that meeting. Meetings planned
// elsewhere in Talk or in another calendar stay read-only.
func (p *Plugin) attachEditable(event *upcomingEvent, roomName, userID string) {
	if roomName == "" {
		return
	}
	m, err := p.getMeetingByIndex(keyRoom + roomName)
	if err != nil || m.Schedule == nil || m.Status != statusScheduled || !p.canControl(userID, m) {
		return
	}
	event.MeetingID = m.ID
	event.ChannelID = m.ChannelID
}

// respondInvitation accepts or declines an invitation from Mattermost with
// the invitee's personal key. The key of the space cannot answer for anyone.
func (p *Plugin) respondInvitation(userID, meetingID string, accept bool) error {
	m, err := p.getMeeting(meetingID)
	if errors.Is(err, errMeetingNotFound) {
		return newUserError(http.StatusNotFound, "Встреча не найдена.")
	}
	if err != nil {
		return err
	}
	if !p.canView(userID, m) {
		return errForbidden
	}
	if m.Schedule == nil || m.Status != statusScheduled || m.Schedule.EventID == "" {
		return newUserError(http.StatusBadRequest, "Ответить можно только на запланированную встречу.")
	}
	if !slices.Contains(m.Schedule.UserIDs, userID) {
		return newUserError(http.StatusForbidden, "Вы не приглашены на эту встречу.")
	}
	client := p.personalClient(userID)
	if client == nil {
		return newCodedError(http.StatusConflict, codePersonalKey, "Чтобы отвечать на приглашения в Mattermost, подключите личный ключ Толка в «Настройки → Контур.Толк».")
	}
	ctx, cancel := apiContext()
	defer cancel()
	if err := client.RespondToEvent(ctx, m.Schedule.EventID, accept); err != nil {
		if talk.IsStatus(err, http.StatusUnauthorized) {
			return p.calendarError(client, userID, "respond to invitation", err)
		}
		p.API.LogWarn("Invitation answer refused", "meeting_id", m.ID, "error", err.Error())
		return newUserError(http.StatusBadGateway, "Толк не принял ответ. Если пространство работает с Outlook или другим внешним календарём, ответьте на приглашение в нём.")
	}
	p.audit("respondInvitation", userID, m.ID)
	if accept && m.Schedule.Recurring {
		go p.acceptOccurrences(m, userID, client)
	}
	// Show the answer now instead of at the next calendar refresh.
	if updated, _, err := p.updateMeeting(m.ID, func(fresh *Meeting) bool {
		if fresh.Schedule == nil {
			return false
		}
		fresh.Schedule.AttendeesAt = time.Time{}
		return true
	}); err == nil {
		p.syncSchedule(nil, updated, time.Now().UTC())
	}
	return nil
}

// occurrenceHorizon is how far ahead an accepted series is reconciled.
const occurrenceHorizon = 8 * 7 * 24 * time.Hour

// acceptOccurrences carries an accepted series over to its occurrences. Talk
// copies a declined series onto every occurrence and hides them from the
// attendee's calendar, but an accepted series leaves occurrences as they were,
// so a change of mind would keep the meetings declined and hidden.
func (p *Plugin) acceptOccurrences(m *Meeting, userID string, personal *talk.Client) {
	key := m.Schedule.InviteeKeys[userID]
	organizer, err := p.calendarClientFor(m)
	if key == "" || err != nil {
		return
	}
	now := time.Now().UTC()
	for from := now; from.Before(now.Add(occurrenceHorizon)); from = from.Add(talk.MaxCalendarPeriod) {
		ctx, cancel := apiContext()
		items, err := organizer.ListEvents(ctx, m.Schedule.Email, from, from.Add(talk.MaxCalendarPeriod))
		cancel()
		if err != nil {
			p.API.LogWarn("Occurrence lookup failed", "meeting_id", m.ID, "error", err.Error())
			return
		}
		for i := range items {
			if items[i].RoomName != m.RoomName || items[i].ID == m.Schedule.EventID || !answeredOtherwise(&items[i], key) {
				continue
			}
			ctx, cancel := apiContext()
			err := personal.RespondToEvent(ctx, items[i].ID, true)
			cancel()
			if err != nil {
				p.API.LogWarn("Occurrence answer refused", "meeting_id", m.ID, "event_id", items[i].ID, "error", err.Error())
				return
			}
		}
	}
}

// answeredOtherwise reports an occurrence where the user has not accepted.
func answeredOtherwise(item *talk.CalendarItem, key string) bool {
	for _, attendee := range attendeesFrom(item) {
		if attendee.Key == key {
			return attendee.Status != "accepted"
		}
	}
	return false
}

// inviteeKeys maps invited users to their Talk keys. Lookups are cached, so
// this costs no Talk calls for people seen before.
func (p *Plugin) inviteeKeys(userIDs []string) map[string]string {
	if len(userIDs) == 0 {
		return nil
	}
	users, appErr := p.API.GetUsersByIds(userIDs)
	if appErr != nil {
		return nil
	}
	found, err := p.resolveTalkUsers(users)
	if err != nil {
		p.API.LogDebug("Invitee lookup failed", "error", err.Error())
	}
	out := make(map[string]string, len(found))
	for userID, identity := range found {
		out[userID] = identity.Key
	}
	return out
}
