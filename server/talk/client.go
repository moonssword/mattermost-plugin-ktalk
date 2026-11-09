// Package talk is a typed client for the Kontur Talk public API.
// Field names follow the public API description at
// https://developer.kontur.ru/doc/talk.public.api.
package talk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const requestTimeout = 20 * time.Second

// Error is a non-2xx response from the API.
type Error struct {
	Status  int
	Message string
	Method  string
	Path    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("talk api: %s %s: HTTP %d: %s", e.Method, e.Path, e.Status, e.Message)
}

// IsStatus reports whether err is an API error with one of the given codes.
func IsStatus(err error, codes ...int) bool {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, code := range codes {
		if apiErr.Status == code {
			return true
		}
	}
	return false
}

// IsNotFound reports whether err is an HTTP 404 from the API.
func IsNotFound(err error) bool { return IsStatus(err, http.StatusNotFound) }

// Client talks to one Talk space. Create a copy with WithToken to act with a
// user's personal key.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	// personal marks a personal access token. Talk serves its owner's
	// calendar under /Calendar, while application keys open any calendar
	// by address under /EmailCalendar/{email}.
	personal bool
}

// NewClient returns a client for a space address like https://x.ktalk.ru.
func NewClient(spaceURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(spaceURL, "/") + "/api",
		token:   token,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// WithToken returns a client for the same space using another key.
func (c *Client) WithToken(token string) *Client {
	clone := *c
	clone.token = token
	clone.personal = false
	return &clone
}

// WithPersonalToken returns a copy that acts as the owner of a personal
// access token. Calendar calls then go to that person's own calendar and the
// address argument is ignored.
func (c *Client) WithPersonalToken(token string) *Client {
	clone := c.WithToken(token)
	clone.personal = true
	return clone
}

// Personal reports that the client uses a personal access token.
func (c *Client) Personal() bool { return c.personal }

// calendarPath is the calendar resource for an organizer and optional tail.
func (c *Client) calendarPath(email string, tail ...string) string {
	path := "/EmailCalendar/" + seg(email)
	if c.personal {
		path = "/Calendar"
	}
	for _, part := range tail {
		path += "/" + seg(part)
	}
	return path
}

// Profile is the owner of a personal access token.
type Profile struct {
	Firstname  string `json:"firstname"`
	Surname    string `json:"surname"`
	Patronymic string `json:"patronymic"`
}

// Name is the display name of the profile.
func (p *Profile) Name() string {
	return strings.TrimSpace(p.Firstname + " " + p.Surname)
}

// GetProfile returns the owner of a personal access token.
func (c *Client) GetProfile(ctx context.Context) (*Profile, error) {
	var out Profile
	if err := c.do(ctx, http.MethodGet, "/Users/profile", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("talk api: encode %s: %w", path, err)
		}
		body = bytes.NewReader(data)
	}

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return fmt.Errorf("talk api: build %s: %w", path, err)
	}
	req.Header.Set("X-Auth-Token", c.token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("talk api: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("talk api: read %s: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{Status: resp.StatusCode, Message: errorMessage(data), Method: method, Path: path}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("talk api: decode %s: %w", path, err)
	}
	return nil
}

func errorMessage(data []byte) string {
	var payload struct {
		ErrorMessage string `json:"errorMessage"`
		Message      string `json:"message"`
		Title        string `json:"title"`
	}
	if json.Unmarshal(data, &payload) == nil {
		for _, msg := range []string{payload.ErrorMessage, payload.Message, payload.Title} {
			if msg != "" {
				return msg
			}
		}
	}
	const limit = 200
	if len(data) > limit {
		data = data[:limit]
	}
	return string(data)
}

func seg(s string) string { return url.PathEscape(s) }

func timeParam(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// --- API keys and statistics ---

// Scope is one access area of an API key.
type Scope struct {
	Type            string `json:"type"`
	RestrictionType string `json:"restrictionType"`
}

// AccessInfo describes the current key.
type AccessInfo struct {
	ExpiredAt time.Time `json:"expiredAt"`
	Scopes    []Scope   `json:"scopes"`
}

// CanRead reports whether the key has the scope at any level.
func (a *AccessInfo) CanRead(scope string) bool {
	for _, s := range a.Scopes {
		if s.Type == scope {
			return true
		}
	}
	return false
}

// CanWrite reports whether the key has the scope with write access.
func (a *AccessInfo) CanWrite(scope string) bool {
	for _, s := range a.Scopes {
		if s.Type == scope && s.RestrictionType == "readWrite" {
			return true
		}
	}
	return false
}

// GetAccessInfo returns the expiry and scopes of the current key. It needs no
// scope and works for expired keys.
func (c *Client) GetAccessInfo(ctx context.Context) (*AccessInfo, error) {
	var out AccessInfo
	if err := c.do(ctx, http.MethodGet, "/domain/applications/access-info", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// OnlineStats is the space-wide live counter.
type OnlineStats struct {
	UsersCount       int `json:"usersCount"`
	ConferencesCount int `json:"conferencesCount"`
}

// GetOnlineStats returns live conference counters.
func (c *Client) GetOnlineStats(ctx context.Context) (*OnlineStats, error) {
	var out OnlineStats
	if err := c.do(ctx, http.MethodGet, "/domain/stats/conferences/online", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTariffExpiration returns the tariff end date, nil when not set.
func (c *Client) GetTariffExpiration(ctx context.Context) (*time.Time, error) {
	var out *time.Time
	if err := c.do(ctx, http.MethodGet, "/domain/stats/tariffExpirationDate", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// --- Users ---

// User is a Talk profile.
type User struct {
	Key       string `json:"key"`
	Login     string `json:"login,omitempty"`
	Firstname string `json:"firstname,omitempty"`
	Surname   string `json:"surname,omitempty"`
	Email     string `json:"email,omitempty"`
	AvatarURL string `json:"avatarUrl,omitempty"`
	Post      string `json:"post,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
	Status    string `json:"status,omitempty"` // unknown, inMeeting, doNotDisturb, online
}

// DisplayName returns the best available name.
func (u *User) DisplayName() string {
	name := strings.TrimSpace(u.Firstname + " " + u.Surname)
	if name != "" {
		return name
	}
	return u.Login
}

// FindUsersByEmail looks users up by address. With withStatus the response
// includes meeting status (online is not reported by this flag in the docs).
func (c *Client) FindUsersByEmail(ctx context.Context, emails []string, withStatus bool) ([]User, error) {
	if len(emails) == 0 {
		return nil, nil
	}
	query := url.Values{"email": emails}
	if withStatus {
		query.Set("fillInMeetingStatus", "true")
	}
	var out struct {
		Users []User `json:"users"`
	}
	if err := c.do(ctx, http.MethodGet, "/Users", query, nil, &out); err != nil {
		return nil, err
	}
	return out.Users, nil
}

// GetUser returns a profile by user key.
func (c *Client) GetUser(ctx context.Context, key string) (*User, error) {
	var out User
	if err := c.do(ctx, http.MethodGet, "/Users/"+seg(key), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- Rooms ---

// Input policies for microphone, camera and screen sharing.
const (
	PolicyNone     = "none"
	PolicyMuted    = "muted"
	PolicyDisabled = "disabled"
)

// AutoRecording configures automatic recording. InitiatorUserKey is required
// when enabling with an API key.
type AutoRecording struct {
	Enabled          bool   `json:"enabled"`
	InitiatorUserKey string `json:"initiatorUserKey,omitempty"`
}

// RoomParams is the body of PUT /Rooms/{roomName}.
type RoomParams struct {
	Title                         string         `json:"title,omitempty"`
	Description                   string         `json:"description,omitempty"`
	ModeratorKeys                 []string       `json:"moderatorKeys,omitempty"`
	AnonymousAccessExpirationDate *time.Time     `json:"anonymousAccessExpirationDate"`
	EnableLobby                   bool           `json:"enableLobby"`
	AudioPolicy                   string         `json:"audioPolicy,omitempty"`
	VideoPolicy                   string         `json:"videoPolicy,omitempty"`
	AutoRecordingSettings         *AutoRecording `json:"autoRecordingSettings,omitempty"`
}

// OnlineUser is a participant currently in a room.
type OnlineUser struct {
	AnonymousName string `json:"anonymousName,omitempty"`
	AnonymousID   string `json:"anonymousId,omitempty"`
	UserInfo      *User  `json:"userInfo,omitempty"`
	IsAnonymous   bool   `json:"isAnonymous"`
	IsModerator   bool   `json:"isModerator"`
}

// Name returns a display name for the participant.
func (o *OnlineUser) Name() string {
	if o.UserInfo != nil {
		return o.UserInfo.DisplayName()
	}
	return o.AnonymousName
}

// SIPSettings holds dial-in details of a room.
type SIPSettings struct {
	SIPNumber    string   `json:"sipNumber,omitempty"`
	SIPURI       string   `json:"sipUri,omitempty"`
	PhoneNumbers []string `json:"phoneNumbers,omitempty"`
}

// Room is the response of GET and PUT /Rooms/{roomName}.
type Room struct {
	RoomName                      string       `json:"roomName"`
	Title                         string       `json:"title,omitempty"`
	ConferenceID                  string       `json:"conferenceId,omitempty"`
	SecurityType                  string       `json:"securityType,omitempty"`
	PinCode                       string       `json:"pinCode,omitempty"`
	EnableLobby                   bool         `json:"enableLobby"`
	AllowAnonymous                bool         `json:"allowAnonymous"`
	AnonymousAccessExpirationDate *time.Time   `json:"anonymousAccessExpirationDate,omitempty"`
	UsersOnline                   int          `json:"usersOnline"`
	OnlineUsers                   []OnlineUser `json:"onlineUsers,omitempty"`
	SIPSettings                   *SIPSettings `json:"sipSettings,omitempty"`
	EnableAutoRecording           *bool        `json:"enableAutoRecording,omitempty"`
	Moderators                    []User       `json:"moderators,omitempty"`
}

// GetRoom returns a room by key.
func (c *Client) GetRoom(ctx context.Context, roomName string) (*Room, error) {
	var out Room
	if err := c.do(ctx, http.MethodGet, "/Rooms/"+seg(roomName), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PutRoom creates or updates a room.
func (c *Client) PutRoom(ctx context.Context, roomName string, params RoomParams) (*Room, error) {
	var out Room
	if err := c.do(ctx, http.MethodPut, "/Rooms/"+seg(roomName), nil, params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetRoomPin sets a 4-6 digit PIN; an empty value removes it.
func (c *Client) SetRoomPin(ctx context.Context, roomName, pin string) error {
	return c.do(ctx, http.MethodPost, "/Rooms/"+seg(roomName)+"/lock", nil, map[string]any{"pinCode": pin}, nil)
}

// SetAnonymousAccess allows guests until the date; nil disables guests.
func (c *Client) SetAnonymousAccess(ctx context.Context, roomName string, until *time.Time) error {
	body := map[string]any{"anonymousAccessExpirationDate": until}
	return c.do(ctx, http.MethodPut, "/Rooms/"+seg(roomName)+"/anonymousAccess", nil, body, nil)
}

// EndConference ends the conference for all participants.
func (c *Client) EndConference(ctx context.Context, roomName string) error {
	return c.do(ctx, http.MethodPost, "/Rooms/"+seg(roomName)+"/endconference", nil, nil, nil)
}

// Callee identifies a person to ring.
type Callee struct {
	UserKey string `json:"userKey,omitempty"`
	Email   string `json:"email,omitempty"`
}

// CalleeResult is the ring outcome for one callee. Status is one of unknown,
// userNotFound, noPhoneNumber, doNotDisturb, alreadyCalling, calling,
// rejected, error.
type CalleeResult struct {
	Callee struct {
		UserKey string `json:"userKey"`
		Email   string `json:"email"`
		User    *User  `json:"user"`
	} `json:"callee"`
	Status        string `json:"status"`
	StatusMessage string `json:"statusMessage,omitempty"`
}

// NotifyCall rings users with an incoming call from the room.
func (c *Client) NotifyCall(ctx context.Context, roomName, title, callerKey string, callees []Callee) ([]CalleeResult, error) {
	body := map[string]any{"callees": callees, "roomTitle": title, "callerUserKey": callerKey}
	var out struct {
		CalleeResults []CalleeResult `json:"calleeResults"`
	}
	if err := c.do(ctx, http.MethodPost, "/Rooms/"+seg(roomName)+"/notifyCall", nil, body, &out); err != nil {
		return nil, err
	}
	return out.CalleeResults, nil
}

// CancelCallFor cancels ringing for some callees only.
func (c *Client) CancelCallFor(ctx context.Context, roomName, callerKey string, callees []Callee) error {
	body := map[string]any{"callerUserKey": callerKey, "callees": callees}
	return c.do(ctx, http.MethodPost, "/Rooms/"+seg(roomName)+"/cancelCall", nil, body, nil)
}

// CancelCall cancels ringing for everyone. Joined participants stay.
func (c *Client) CancelCall(ctx context.Context, roomName, callerKey string) error {
	body := map[string]any{"callerUserKey": callerKey}
	return c.do(ctx, http.MethodPost, "/Rooms/"+seg(roomName)+"/cancelCall", nil, body, nil)
}

// --- Calendar ---

// Recurrence is a repeating meeting pattern. Send either EndDate or
// NumberOfEvents, never both.
type Recurrence struct {
	Type           string     `json:"type,omitempty"` // daily, weekly, monthly, ...
	StartDate      *time.Time `json:"startDate,omitempty"`
	EndDate        *time.Time `json:"endDate,omitempty"`
	NumberOfEvents *int       `json:"numberOfEvents,omitempty"`
	Interval       *int       `json:"interval,omitempty"`
	DayOfMonth     *int       `json:"dayOfMonth,omitempty"`
	DaysOfTheWeek  []string   `json:"daysOfTheWeek,omitempty"`
}

// CalendarEvent is the body of calendar create and edit calls.
type CalendarEvent struct {
	Start                  time.Time   `json:"start"`
	End                    time.Time   `json:"end"`
	Timezone               string      `json:"timezone,omitempty"`
	Subject                string      `json:"subject"`
	Description            string      `json:"description,omitempty"`
	RoomName               string      `json:"roomName"`
	AllowAnonymous         bool        `json:"allowAnonymous"`
	EnableSIP              bool        `json:"enableSip"`
	PinCode                string      `json:"pinCode,omitempty"`
	EnableAutoRecording    bool        `json:"enableAutoRecording"`
	IsRecurring            bool        `json:"isRecurring"`
	Recurrence             *Recurrence `json:"recurrence,omitempty"`
	RequiredUserKeys       []string    `json:"requiredUserKeys,omitempty"`
	RequiredExternalEmails []string    `json:"requiredExternalAttendeesEmails,omitempty"`
	ControlledExternally   *bool       `json:"controlledViaExternalSystem,omitempty"`
}

// Attendee is a person invited to a calendar item.
type Attendee struct {
	User    *User  `json:"user,omitempty"`
	Name    string `json:"name,omitempty"`
	Mailbox string `json:"mailbox,omitempty"`
	Status  string `json:"status,omitempty"` // unknown, accepted, reject
}

// ExternalMeeting is a meeting hosted by another VCS.
type ExternalMeeting struct {
	URL     string `json:"url,omitempty"`
	Type    string `json:"type,omitempty"`
	VCSName string `json:"vcsName,omitempty"`
}

// CalendarItem is an event returned by the calendar API.
type CalendarItem struct {
	ID                string           `json:"id"`
	Subject           string           `json:"subject"`
	Description       string           `json:"description,omitempty"`
	Start             time.Time        `json:"start"`
	End               time.Time        `json:"end"`
	Timezone          string           `json:"timezone,omitempty"`
	RoomName          string           `json:"roomName,omitempty"`
	OnlineMeetingURL  string           `json:"onlineMeetingUrl,omitempty"`
	Organizer         *Attendee        `json:"organizer,omitempty"`
	RequiredAttendees []Attendee       `json:"requiredAttendees,omitempty"`
	OptionalAttendees []Attendee       `json:"optionalAttendees,omitempty"`
	IsCancelled       bool             `json:"isCancelled"`
	IsRecurring       bool             `json:"isRecurring"`
	OnlineUsersCount  int              `json:"onlineUsersCount"`
	ExternalMeeting   *ExternalMeeting `json:"externalMeeting,omitempty"`
	// BusyType is a string such as "busy". The personal calendar list also
	// returns deleted events, not marked as cancelled, with the number 0 here.
	BusyType json.RawMessage `json:"busyType,omitempty"`
}

// Removed reports a deleted event that the personal calendar still lists.
func (i *CalendarItem) Removed() bool {
	raw := strings.TrimSpace(string(i.BusyType))
	return raw != "" && raw[0] >= '0' && raw[0] <= '9'
}

// CreateEvent creates a meeting on the organizer's calendar.
func (c *Client) CreateEvent(ctx context.Context, email string, event CalendarEvent) (*CalendarItem, error) {
	var out CalendarItem
	if err := c.do(ctx, http.MethodPost, c.calendarPath(email), nil, event, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateEvent edits a meeting. Attendees are changed with SetAttendees.
func (c *Client) UpdateEvent(ctx context.Context, email, eventID string, event CalendarEvent) (*CalendarItem, error) {
	var out CalendarItem
	if err := c.do(ctx, http.MethodPut, c.calendarPath(email, eventID), nil, event, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetAttendees replaces the attendees of a meeting.
func (c *Client) SetAttendees(ctx context.Context, email, eventID string, userKeys, externalEmails []string) (*CalendarItem, error) {
	// An empty list must go as [], not null: Talk reads null as "no change"
	// and would keep everyone who was just removed.
	if userKeys == nil {
		userKeys = []string{}
	}
	if externalEmails == nil {
		externalEmails = []string{}
	}
	body := map[string]any{"requiredUserKeys": userKeys, "requiredExternalAttendeesEmails": externalEmails}
	var out CalendarItem
	if err := c.do(ctx, http.MethodPost, c.calendarPath(email, eventID, "attendees"), nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelEvent cancels a meeting with an optional reason.
func (c *Client) CancelEvent(ctx context.Context, email, eventID, message string) error {
	var query url.Values
	if message != "" {
		query = url.Values{"message": {message}}
	}
	if c.personal {
		return c.do(ctx, http.MethodPost, c.calendarPath(email, eventID, "cancel"), query, nil, nil)
	}
	return c.do(ctx, http.MethodDelete, c.calendarPath(email, eventID), query, nil, nil)
}

// ErrPersonalOnly is returned for calls that only a personal token can make.
var ErrPersonalOnly = errors.New("talk api: this call needs a personal access token")

// RespondToEvent accepts or declines an invitation as the token owner. Talk
// supports it only in spaces on the Talk calendar, not on Exchange or CalDAV.
func (c *Client) RespondToEvent(ctx context.Context, eventID string, accept bool) error {
	if !c.personal {
		return ErrPersonalOnly
	}
	if accept {
		return c.do(ctx, http.MethodPost, c.calendarPath("", eventID, "accept"), nil, nil, nil)
	}
	return c.do(ctx, http.MethodPost, c.calendarPath("", eventID, "reject"), url.Values{"withNotification": {"true"}}, nil, nil)
}

// GetSeries returns a recurring meeting as a whole, by the id of the series
// or of any occurrence. Answers to the series live here: each occurrence in the
// list keeps its own answers, which an answer to the series does not change.
func (c *Client) GetSeries(ctx context.Context, email, eventID string) (*CalendarItem, error) {
	var out CalendarItem
	if err := c.do(ctx, http.MethodGet, c.calendarPath(email, eventID, "recurrence"), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MaxCalendarPeriod is the longest period the calendar list call accepts.
const MaxCalendarPeriod = 7 * 24 * time.Hour

// ListEvents returns meetings in a period of at most seven days.
func (c *Client) ListEvents(ctx context.Context, email string, start, end time.Time) ([]CalendarItem, error) {
	if end.Sub(start) > MaxCalendarPeriod {
		end = start.Add(MaxCalendarPeriod)
	}
	query := url.Values{"start": {timeParam(start)}, "end": {timeParam(end)}}
	var out struct {
		Items []CalendarItem `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, c.calendarPath(email), query, nil, &out); err != nil {
		return nil, err
	}
	items := out.Items[:0]
	for i := range out.Items {
		if !out.Items[i].Removed() {
			items = append(items, out.Items[i])
		}
	}
	return items, nil
}

// --- Recordings ---

// Recording is a space recording.
type Recording struct {
	ID                string     `json:"id,omitempty"`
	Key               string     `json:"key"`
	Title             string     `json:"title"`
	CreatedDate       time.Time  `json:"createdDate"`
	CreatedBy         *User      `json:"createdBy,omitempty"`
	RoomName          string     `json:"roomName,omitempty"`
	ParticipantsCount int        `json:"participantsCount"`
	Size              *int64     `json:"size,omitempty"`
	Duration          int        `json:"duration"`
	Participants      []UserRef  `json:"participants,omitempty"`
	ModifiedDate      *time.Time `json:"modifiedDate,omitempty"`
}

// UserRef references a user or an anonymous participant.
type UserRef struct {
	AnonymousName string `json:"anonymousName,omitempty"`
	AnonymousID   string `json:"anonymousId,omitempty"`
	UserInfo      *User  `json:"userInfo,omitempty"`
	IsAnonymous   bool   `json:"isAnonymous"`
}

// Name returns a display name for the reference.
func (r *UserRef) Name() string {
	if r.UserInfo != nil {
		return r.UserInfo.DisplayName()
	}
	return r.AnonymousName
}

// RecordingFilter narrows ListRecordings.
type RecordingFilter struct {
	Query     string
	StartFrom *time.Time
	PageToken string
	Top       int
}

// RecordingPage is one page of recordings.
type RecordingPage struct {
	Entities      []Recording `json:"entities"`
	NextPageToken string      `json:"nextPageToken"`
	PrevPageToken string      `json:"prevPageToken"`
}

// ListRecordings returns space recordings, newest first.
func (c *Client) ListRecordings(ctx context.Context, f RecordingFilter) (*RecordingPage, error) {
	query := url.Values{"orderMode": {"byTimeNewFirst"}, "maxParticipantCount": {"10"}}
	if f.Query != "" {
		query.Set("query", f.Query)
	}
	if f.StartFrom != nil {
		query.Set("startFrom", timeParam(*f.StartFrom))
	}
	if f.PageToken != "" {
		query.Set("pageTokenString", f.PageToken)
	}
	if f.Top > 0 {
		query.Set("top", fmt.Sprint(f.Top))
	}
	var out RecordingPage
	if err := c.do(ctx, http.MethodGet, "/Domain/recordings/v2", query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRecording returns one recording.
func (c *Client) GetRecording(ctx context.Context, key string) (*Recording, error) {
	var out Recording
	if err := c.do(ctx, http.MethodGet, "/Domain/recordings/"+seg(key), url.Values{"maxParticipantCount": {"10"}}, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StartRecording starts cloud recording in a room and returns the key.
func (c *Client) StartRecording(ctx context.Context, roomName, title, initiatorKey string) (string, error) {
	body := map[string]any{"roomName": roomName, "title": title}
	if initiatorKey != "" {
		body["initiatorUserKey"] = initiatorKey
	}
	var out struct {
		RecordingKey string `json:"recordingKey"`
	}
	if err := c.do(ctx, http.MethodPost, "/Recordings/start", nil, body, &out); err != nil {
		return "", err
	}
	return out.RecordingKey, nil
}

// GetActiveRecording returns the key of the running recording, "" when none.
func (c *Client) GetActiveRecording(ctx context.Context, roomName string) (string, error) {
	var out struct {
		RecordingKey string `json:"recordingKey"`
	}
	err := c.do(ctx, http.MethodGet, "/Recordings/current", url.Values{"roomName": {roomName}}, nil, &out)
	if IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return out.RecordingKey, nil
}

// SetRecordingLinkAccess sets link access to none, domain or global, keeping
// individual accesses as they are.
func (c *Client) SetRecordingLinkAccess(ctx context.Context, key, scope string) error {
	body := map[string]any{"userAccesses": []any{}, "linkAccess": map[string]string{"scope": scope}}
	return c.do(ctx, http.MethodPatch, "/Recordings/"+seg(key)+"/access", nil, body, nil)
}

// Statuses of speech artifacts.
const (
	SpeechSuccess    = "success"
	SpeechInProgress = "inProgress"
)

// SummaryChunk is one part of a summary.
type SummaryChunk struct {
	Type      string `json:"type"`
	Timestamp int    `json:"timestamp"`
	Text      string `json:"text"`
}

// Summary is a short summary or a protocol.
type Summary struct {
	Status string         `json:"status"`
	Chunks []SummaryChunk `json:"chunks"`
	Hidden bool           `json:"hidden"`
}

// Text joins the visible chunks.
func (s *Summary) Text() string {
	if s == nil || s.Hidden {
		return ""
	}
	parts := make([]string, 0, len(s.Chunks))
	for _, chunk := range s.Chunks {
		if text := strings.TrimSpace(chunk.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// TranscriptChunk is one phrase.
type TranscriptChunk struct {
	StartMillis int    `json:"startTimeOffsetInMillis"`
	EndMillis   int    `json:"endTimeOffsetInMillis"`
	Text        string `json:"text"`
}

// TranscriptTrack is the speech of one speaker.
type TranscriptTrack struct {
	Speaker *UserRef          `json:"speaker,omitempty"`
	Chunks  []TranscriptChunk `json:"chunks"`
}

// Transcript is the transcription of a recording.
type Transcript struct {
	Status string            `json:"status"`
	Tracks []TranscriptTrack `json:"tracks"`
}

// SpeechArtifacts bundles transcript, summary and protocol.
type SpeechArtifacts struct {
	Transcript   *Transcript `json:"transcriptionV2,omitempty"`
	ShortSummary *Summary    `json:"shortSummaryV2,omitempty"`
	Protocol     *Summary    `json:"protocolV2,omitempty"`
}

// GetSpeechArtifacts returns transcript, short summary and protocol at once.
func (c *Client) GetSpeechArtifacts(ctx context.Context, key string) (*SpeechArtifacts, error) {
	var out SpeechArtifacts
	if err := c.do(ctx, http.MethodGet, "/recordings/v2/"+seg(key)+"/summary", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- Conference history ---

// ConferenceInfo is a short record of a conference.
type ConferenceInfo struct {
	Key       string     `json:"key"`
	RoomName  string     `json:"roomName"`
	StartTime time.Time  `json:"startTime"`
	EndTime   *time.Time `json:"endTime,omitempty"`
	Title     string     `json:"title,omitempty"`
}

// ListRoomConferences returns conferences in a room since the given time.
func (c *Client) ListRoomConferences(ctx context.Context, roomName string, from time.Time) ([]ConferenceInfo, error) {
	query := url.Values{"roomName": {roomName}, "fromDate": {timeParam(from)}, "take": {"20"}}
	var out struct {
		Conferences []ConferenceInfo `json:"conferences"`
	}
	if err := c.do(ctx, http.MethodGet, "/domain/conferencesHistory", query, nil, &out); err != nil {
		return nil, err
	}
	return out.Conferences, nil
}

// ConferenceArtifacts are the materials of a finished conference.
type ConferenceArtifacts struct {
	Participants        []UserRef `json:"participants"`
	InvitedParticipants []UserRef `json:"invitedParticipants"`
	Recordings          []struct {
		ID string `json:"id"`
	} `json:"recordings"`
	Polls       []struct{} `json:"polls"`
	Notes       []struct{} `json:"notes"`
	Whiteboards []struct{} `json:"whiteboards"`
}

// ConferenceDetails is the enriched record of a conference.
type ConferenceDetails struct {
	ConferenceInfo
	Artifacts *ConferenceArtifacts `json:"artifacts,omitempty"`
}

// GetConference returns a finished conference with its artifacts.
func (c *Client) GetConference(ctx context.Context, key string) (*ConferenceDetails, error) {
	var out ConferenceDetails
	if err := c.do(ctx, http.MethodGet, "/ConferencesHistory/v2/"+seg(key), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- Webhooks ---

// WebhookEvents are all subscription names, in the camelCase the API expects.
var WebhookEvents = []string{
	"conferencesStarted", "conferencesFinished", "recordingCompleted",
	"transcriptionReady", "userConnectedToRoom", "userDisconnectedFromRoom",
}

// WebhookHeader is a header Talk adds to each delivery.
type WebhookHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Webhook is a webhook registration.
type Webhook struct {
	WebhookKey string   `json:"webhookKey"`
	Title      string   `json:"title"`
	Activated  bool     `json:"activated"`
	Events     []string `json:"events"`
	URL        string   `json:"url"`
}

// ListWebhooks returns registered webhooks.
func (c *Client) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	var out []Webhook
	if err := c.do(ctx, http.MethodGet, "/Webhooks", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateWebhook registers a webhook. The API allows ten calls a day.
func (c *Client) CreateWebhook(ctx context.Context, title, target string, headers []WebhookHeader) (*Webhook, error) {
	body := map[string]any{"title": title, "url": target, "events": WebhookEvents, "customHeaders": headers}
	var out Webhook
	if err := c.do(ctx, http.MethodPost, "/Webhooks", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ActivateWebhook enables delivery with the key sent to the webhook URL.
func (c *Client) ActivateWebhook(ctx context.Context, webhookKey, activationKey string) error {
	body := map[string]string{"activationKey": activationKey}
	return c.do(ctx, http.MethodPost, "/Webhooks/"+seg(webhookKey)+"/activate", nil, body, nil)
}

// DeleteWebhook removes a webhook.
func (c *Client) DeleteWebhook(ctx context.Context, webhookKey string) error {
	return c.do(ctx, http.MethodDelete, "/Webhooks/"+seg(webhookKey), nil, nil, nil)
}
