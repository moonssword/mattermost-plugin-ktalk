package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

const maxTitleLength = 128

var markdownLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// userError is safe to show to the person who triggered it. Code lets the
// webapp tell known situations apart and show them as a notice, not an error.
type userError struct {
	status  int
	message string
	code    string
}

func (e *userError) Error() string { return e.message }

func newUserError(status int, message string) *userError {
	return &userError{status: status, message: message}
}

// Codes the webapp knows.
const (
	codeEmailUnverified = "email_unverified"
	codeNotInTalk       = "not_in_talk"
	codePersonalKey     = "personal_key_required"
)

func newCodedError(status int, code, message string) *userError {
	return &userError{status: status, message: message, code: code}
}

var errForbidden = newUserError(http.StatusForbidden, "Недостаточно прав для этого действия.")

// describeAPIError turns a Talk API failure into a message for users. Details
// stay in the server log.
func (p *Plugin) describeAPIError(action string, err error) *userError {
	p.noteAPIError(err)
	p.API.LogError("Talk API call failed", "action", action, "error", err.Error())
	var known *userError
	if errors.As(err, &known) {
		return known
	}
	switch {
	case talk.IsStatus(err, http.StatusUnauthorized):
		return newUserError(http.StatusBadGateway, "Контур.Толк не принял API-ключ. Обратитесь к администратору.")
	case talk.IsStatus(err, http.StatusForbidden):
		return newUserError(http.StatusBadGateway, "У API-ключа Контур.Толка нет прав на это действие. Обратитесь к администратору.")
	case talk.IsStatus(err, http.StatusTooManyRequests):
		return newUserError(http.StatusBadGateway, "Контур.Толк ограничил число запросов. Повторите чуть позже.")
	default:
		return newUserError(http.StatusBadGateway, "Не удалось выполнить действие в Контур.Толке. Повторите чуть позже.")
	}
}

// createRequest describes a meeting to start.
type createRequest struct {
	ChannelID string `json:"channel_id"`
	RootID    string `json:"root_id"`
	PostID    string `json:"post_id"`
	Title     string `json:"title"`
	RoomName  string `json:"room_name"`
	// LinkOnly posts the meeting card in a direct or group chat without
	// ringing anyone. Other channels never ring.
	LinkOnly bool `json:"link_only"`
}

func (p *Plugin) checkCanStart(user *model.User, channelID string) error {
	if p.getConfig().spaceURL() == "" {
		return newUserError(http.StatusServiceUnavailable, "Плагин не настроен: укажите адрес пространства в системной консоли.")
	}
	if user.IsGuest() && !p.getConfig().AllowGuests {
		return newUserError(http.StatusForbidden, "Гостям Mattermost запуск встреч недоступен.")
	}
	if !p.API.HasPermissionToChannel(user.Id, channelID, model.PermissionCreatePost) {
		return errForbidden
	}
	return nil
}

// startMeeting creates the room and the channel card.
func (p *Plugin) startMeeting(userID string, req createRequest) (*Meeting, error) {
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return nil, fmt.Errorf("get user: %w", appErr)
	}
	if req.PostID != "" {
		post, postErr := p.API.GetPost(req.PostID)
		if postErr != nil {
			return nil, newUserError(http.StatusNotFound, "Сообщение не найдено.")
		}
		req.ChannelID = post.ChannelId
		req.RootID = post.RootId
		if req.RootID == "" {
			req.RootID = post.Id
		}
		if req.Title == "" {
			req.Title = firstLine(post.Message)
		}
	}
	if err := p.checkCanStart(user, req.ChannelID); err != nil {
		return nil, err
	}
	channel, appErr := p.API.GetChannel(req.ChannelID)
	if appErr != nil {
		return nil, newUserError(http.StatusNotFound, "Канал не найден.")
	}

	cfg := p.getConfig()
	roomName := req.RoomName
	if roomName != "" && !validRoomName(roomName) {
		return nil, newUserError(http.StatusBadRequest, "Ключ комнаты: от 3 до 64 символов, только строчные латинские буквы и цифры.")
	}
	if roomName == "" {
		roomName = p.newRoomName(user.Id, channel)
	}

	title := clip(req.Title, maxTitleLength)
	if title == "" {
		title = defaultTitle(channel, user, req.LinkOnly)
	}

	kind := kindAdhoc
	if (channel.Type == model.ChannelTypeDirect || channel.Type == model.ChannelTypeGroup) && cfg.EnableCalls && !req.LinkOnly {
		kind = kindCall
	}

	now := time.Now().UTC()
	meeting := &Meeting{
		ID:          model.NewId(),
		RoomName:    roomName,
		URL:         cfg.roomURL(roomName),
		Title:       title,
		ChannelID:   channel.Id,
		RootID:      req.RootID,
		CreatorID:   user.Id,
		CreatorName: user.GetDisplayName(model.ShowFullName),
		Kind:        kind,
		Status:      statusWaiting,
		CreatedAt:   now,
	}

	members := p.channelUsers(channel.Id)
	if err := p.provisionRoom(meeting, channel, user, members, now); err != nil {
		return nil, err
	}
	if err := p.publishMeeting(meeting); err != nil {
		return nil, err
	}
	p.metrics.meetingsCreated.Add(1)
	p.audit("startMeeting", user.Id, meeting.ID)

	if kind == kindCall {
		p.ringChannel(meeting.ID, user, members)
		if updated, err := p.getMeeting(meeting.ID); err == nil {
			meeting = updated
		}
	}
	return meeting, nil
}

func (p *Plugin) newRoomName(userID string, channel *model.Channel) string {
	cfg := p.getConfig()
	scheme := p.userPref(userID, prefNaming, cfg.NamingScheme)
	if scheme == schemeAsk {
		scheme = schemeWords // The dialog handles asking; this is the fallback.
	}
	teamName := ""
	if channel.TeamId != "" {
		if team, appErr := p.API.GetTeam(channel.TeamId); appErr == nil {
			teamName = team.Name
		}
	}
	channelName := channel.Name
	if channel.Type == model.ChannelTypeDirect || channel.Type == model.ChannelTypeGroup {
		channelName = "dm"
	}
	return generateRoomName(scheme, cfg.RoomPrefix, teamName, channelName)
}

func defaultTitle(channel *model.Channel, user *model.User, linkOnly bool) string {
	if channel.Type == model.ChannelTypeDirect || channel.Type == model.ChannelTypeGroup {
		if linkOnly {
			return "Встреча: " + user.GetDisplayName(model.ShowFullName)
		}
		return "Звонок: " + user.GetDisplayName(model.ShowFullName)
	}
	return "Встреча: " + channel.DisplayName
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return s
}

// plainTitle drops the usual Markdown marks. Cards render titles as Markdown,
// but Talk shows them as they are, so what goes to Talk must be plain.
func plainTitle(s string) string {
	s = firstLine(s)
	s = markdownLink.ReplaceAllString(s, "$1")
	s = strings.NewReplacer("**", "", "__", "", "~~", "", "`", "").Replace(s)
	return strings.TrimSpace(strings.TrimLeft(s, "#>*- "))
}

// channelUsers returns up to maxCallees+1 non-bot members.
func (p *Plugin) channelUsers(channelID string) []*model.User {
	members, appErr := p.API.GetChannelMembers(channelID, 0, 200)
	if appErr != nil {
		p.API.LogWarn("Failed to list channel members", "channel_id", channelID, "error", appErr.Error())
		return nil
	}
	ids := make([]string, 0, len(members))
	for i := range members {
		ids = append(ids, members[i].UserId)
	}
	if len(ids) == 0 {
		return nil
	}
	users, appErr := p.API.GetUsersByIds(ids)
	if appErr != nil {
		p.API.LogWarn("Failed to load channel users", "channel_id", channelID, "error", appErr.Error())
		return nil
	}
	kept := users[:0]
	for _, user := range users {
		if !user.IsBot && user.DeleteAt == 0 {
			kept = append(kept, user)
		}
	}
	return kept
}

// provisionRoom applies the channel policy to the Talk room. Without an API
// key the meeting stays a plain link.
func (p *Plugin) provisionRoom(m *Meeting, channel *model.Channel, user *model.User, members []*model.User, now time.Time) error {
	client := p.client()
	if client == nil {
		m.LinkOnly = true
		return nil
	}
	var memberCount int64
	if stats, appErr := p.API.GetChannelStats(channel.Id); appErr == nil {
		memberCount = stats.MemberCount
	}
	policy := computeRoomPolicy(string(channel.Type), memberCount, p.getConfig(), now)
	m.Lobby = policy.Lobby
	m.GuestsUntil = policy.GuestsUntil
	m.Muted = policy.Muted

	// In direct and group channels everybody is a moderator, elsewhere only
	// the person who started the meeting.
	lookup := []*model.User{user}
	if channel.Type == model.ChannelTypeDirect || channel.Type == model.ChannelTypeGroup {
		lookup = members
	}
	identities, err := p.resolveTalkUsers(lookup)
	if err != nil {
		return p.describeAPIError("resolve users", err)
	}
	if identity, ok := identities[user.Id]; ok {
		m.CreatorKey = identity.Key
	} else if identity, ok := p.talkUser(user); ok {
		m.CreatorKey = identity.Key
	}
	seen := map[string]bool{}
	for _, identity := range identities {
		if !seen[identity.Key] {
			m.Moderators = append(m.Moderators, identity.Key)
			seen[identity.Key] = true
		}
	}

	ctx, cancel := apiContext()
	defer cancel()
	room, err := client.PutRoom(ctx, m.RoomName, roomParamsFor(m))
	if err != nil {
		return p.describeAPIError("put room", err)
	}
	if room.SIPSettings != nil && (room.SIPSettings.SIPNumber != "" || len(room.SIPSettings.PhoneNumbers) > 0) {
		m.SIP = room.SIPSettings
	}
	return nil
}

// roomParamsFor rebuilds the full room settings from the meeting. PUT replaces
// fields such as the lobby flag, so every call sends all of them.
func roomParamsFor(m *Meeting) talk.RoomParams {
	params := talk.RoomParams{
		Title:                         plainTitle(m.Title),
		ModeratorKeys:                 m.Moderators,
		AnonymousAccessExpirationDate: m.GuestsUntil,
		EnableLobby:                   m.Lobby,
	}
	if m.Muted {
		params.AudioPolicy = talk.PolicyMuted
		params.VideoPolicy = talk.PolicyMuted
	}
	// A scheduled meeting states auto-recording either way, so an edit can
	// also turn it off.
	if m.Schedule != nil && m.CreatorKey != "" {
		params.AutoRecordingSettings = &talk.AutoRecording{Enabled: m.Schedule.AutoRecord}
		if m.Schedule.AutoRecord {
			params.AutoRecordingSettings.InitiatorUserKey = m.CreatorKey
		}
	}
	return params
}

// publishMeeting posts the card and stores the meeting.
func (p *Plugin) publishMeeting(m *Meeting) error {
	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: m.ChannelID,
		RootId:    m.RootID,
		Type:      postTypeCard,
		Message:   cardFallback(m),
	}
	post.AddProp("meeting", m.view())
	created, appErr := p.API.CreatePost(post)
	if appErr != nil {
		return fmt.Errorf("create card: %w", appErr)
	}
	m.PostID = created.Id
	if err := p.saveNewMeeting(m); err != nil {
		return err
	}
	// The post id only exists now, and clients read it from the view.
	p.refreshCard(m)
	return nil
}

// cardFallback is what clients without the plugin webapp, such as mobile, show.
func cardFallback(m *Meeting) string {
	// The title may hold Markdown of its own, which would nest in the bold.
	title := safeInline(plainTitle(m.Title))
	switch m.Status {
	case statusCancelled:
		return fmt.Sprintf("Встреча **%s** отменена.", title)
	case statusEnded:
		return fmt.Sprintf("Встреча **%s** завершена.", title)
	case statusScheduled:
		return fmt.Sprintf("Запланирована встреча **%s** на %s UTC. [Присоединиться](%s)", title, m.Schedule.Start.UTC().Format("02.01.2006 15:04"), m.URL)
	default:
		return fmt.Sprintf("Встреча **%s** в Контур.Толке. [Присоединиться](%s)", title, m.URL)
	}
}

// refreshCard writes the meeting into its post and tells clients about it.
func (p *Plugin) refreshCard(m *Meeting) {
	if m.PostID == "" {
		return
	}
	post, appErr := p.API.GetPost(m.PostID)
	if appErr != nil {
		p.API.LogWarn("Card post unavailable", "post_id", m.PostID, "error", appErr.Error())
		return
	}
	post.Message = cardFallback(m)
	post.AddProp("meeting", m.view())
	if _, appErr := p.API.UpdatePost(post); appErr != nil {
		p.API.LogWarn("Failed to update card", "post_id", m.PostID, "error", appErr.Error())
	}
	p.API.PublishWebSocketEvent(wsMeeting, map[string]any{"meeting_id": m.ID, "channel_id": m.ChannelID, "status": m.Status},
		&model.WebsocketBroadcast{ChannelId: m.ChannelID})
}

// ringChannel rings the other members of a direct or group channel. The call
// rings in Mattermost for everyone, and also in the Talk app for people who
// have a Talk account when the administrator allows it. Ringing is best
// effort: if it fails, the card is still there and the meeting works.
func (p *Plugin) ringChannel(meetingID string, caller *model.User, members []*model.User) {
	cfg := p.getConfig()
	meeting, err := p.getMeeting(meetingID)
	if err != nil {
		return
	}

	var identities map[string]talkIdentity
	if cfg.RingInTalk && p.client() != nil {
		identities, err = p.resolveTalkUsers(members)
		if err != nil {
			p.API.LogWarn("Ring: user lookup failed", "error", err.Error())
		}
	}

	callees := make([]CallCallee, 0, len(members))
	for _, member := range members {
		if member.Id == caller.Id || len(callees) >= maxCallees || !cfg.allowsUser(member) {
			continue // A test must not ring people who are outside it.
		}
		callee := CallCallee{UserID: member.Id, Name: member.GetDisplayName(model.ShowFullName), Status: calleeCalling}
		if p.userPref(member.Id, prefAcceptCalls, "true") != "true" {
			callee.Status = calleeCallsOff
		}
		if identity, ok := identities[member.Id]; ok {
			callee.TalkKey = identity.Key
		}
		callees = append(callees, callee)
	}

	ringing := false
	for i := range callees {
		if callees[i].Status == calleeCalling {
			ringing = true
		}
	}
	if !ringing {
		p.setCallees(meetingID, callees, false)
		return
	}

	if callerIdentity, ok := identities[caller.Id]; ok {
		p.ringInTalk(meeting, callerIdentity.Key, callees)
	}
	p.setCallees(meetingID, callees, true)
	p.metrics.callsStarted.Add(1)

	expires := time.Now().Add(ringingTimeout)
	for i := range callees {
		if callees[i].Status == calleeCalling {
			p.notifyIncoming(meeting, caller, callees[i].UserID, expires)
		}
	}
}

// ringInTalk adds a Talk call for callees who have an account there. Talk's
// answer is kept only when it says the person cannot take calls.
func (p *Plugin) ringInTalk(m *Meeting, callerKey string, callees []CallCallee) {
	client := p.client()
	requests := []talk.Callee{}
	for _, callee := range callees {
		if callee.Status == calleeCalling && callee.TalkKey != "" {
			requests = append(requests, talk.Callee{UserKey: callee.TalkKey})
		}
	}
	if client == nil || len(requests) == 0 {
		return
	}
	ctx, cancel := apiContext()
	defer cancel()
	results, err := client.NotifyCall(ctx, m.RoomName, plainTitle(m.Title), callerKey, requests)
	if err != nil {
		p.noteAPIError(err)
		p.API.LogWarn("Ring in Talk failed", "meeting_id", m.ID, "error", err.Error())
		return
	}
	for i := range callees {
		for j := range results {
			key := results[j].Callee.UserKey
			if key == "" && results[j].Callee.User != nil {
				key = results[j].Callee.User.Key
			}
			if key == callees[i].TalkKey && results[j].Status == "doNotDisturb" {
				callees[i].Status = "doNotDisturb"
			}
		}
	}
}

func (p *Plugin) setCallees(meetingID string, callees []CallCallee, ringing bool) {
	m, changed, err := p.updateMeeting(meetingID, func(m *Meeting) bool {
		m.Callees = callees
		if ringing {
			m.Status = statusRinging
			m.RingingUntil = time.Now().UTC().Add(ringingTimeout)
		}
		return true
	})
	if err != nil {
		p.API.LogWarn("Failed to save call state", "meeting_id", meetingID, "error", err.Error())
		return
	}
	if changed {
		p.refreshCard(m)
	}
}

// incomingCall is what a callee's clients receive.
type incomingCall struct {
	MeetingID  string    `json:"meeting_id"`
	ChannelID  string    `json:"channel_id"`
	CallerID   string    `json:"caller_id"`
	CallerName string    `json:"caller_name"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// notifyIncoming rings a callee in every open Mattermost client and on their
// phone through the Mattermost push service.
func (p *Plugin) notifyIncoming(m *Meeting, caller *model.User, userID string, expires time.Time) {
	call := incomingCall{
		MeetingID: m.ID, ChannelID: m.ChannelID, CallerID: caller.Id,
		CallerName: caller.GetDisplayName(model.ShowFullName), Title: plainTitle(m.Title), URL: m.URL, ExpiresAt: expires,
	}
	payload := map[string]any{}
	data, _ := json.Marshal(call)
	_ = json.Unmarshal(data, &payload)
	p.API.PublishWebSocketEvent(wsIncomingCall, payload, &model.WebsocketBroadcast{UserId: userID})

	push := &model.PushNotification{
		Type:       model.PushTypeMessage,
		ChannelId:  m.ChannelID,
		PostId:     m.PostID,
		SenderId:   caller.Id,
		SenderName: call.CallerName,
		Message:    call.CallerName + " звонит вам в Контур.Толке",
	}
	if appErr := p.API.SendPushNotification(push, userID); appErr != nil {
		p.API.LogDebug("Push for incoming call failed", "user_id", userID, "error", appErr.Error())
	}
}

// dismissIncoming stops ringing in all of a callee's clients.
func (p *Plugin) dismissIncoming(meetingID string, userIDs ...string) {
	for _, userID := range userIDs {
		p.API.PublishWebSocketEvent(wsDismissCall, map[string]any{"meeting_id": meetingID}, &model.WebsocketBroadcast{UserId: userID})
	}
}

// answerCall records a callee's answer. Accepting does not open anything on
// the server: the client opens the room itself.
func (p *Plugin) answerCall(userID, meetingID string, accept bool) error {
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
	status := calleeDeclined
	if accept {
		status = calleeAccepted
	}
	var talkKey string
	updated, changed, err := p.updateMeeting(meetingID, func(fresh *Meeting) bool {
		found := false
		for i := range fresh.Callees {
			if fresh.Callees[i].UserID == userID && fresh.Callees[i].Status == calleeCalling {
				fresh.Callees[i].Status = status
				talkKey = fresh.Callees[i].TalkKey
				found = true
			}
		}
		if found && fresh.Status == statusRinging && !anyCalling(fresh.Callees) {
			fresh.Status = statusWaiting
			fresh.RingingUntil = time.Now().UTC()
		}
		return found
	})
	if err != nil {
		return err
	}
	p.dismissIncoming(meetingID, userID)
	if !changed {
		return nil
	}
	p.refreshCard(updated)
	if !accept && talkKey != "" && p.client() != nil {
		// Stop the Talk app ringing too. Others keep ringing.
		ctx, cancel := apiContext()
		defer cancel()
		if err := p.client().CancelCallFor(ctx, updated.RoomName, updated.CreatorKey, []talk.Callee{{UserKey: talkKey}}); err != nil {
			p.API.LogDebug("Cancel Talk ring failed", "meeting_id", meetingID, "error", err.Error())
		}
	}
	return nil
}

func anyCalling(callees []CallCallee) bool {
	for _, callee := range callees {
		if callee.Status == calleeCalling {
			return true
		}
	}
	return false
}

func callingUsers(callees []CallCallee) []string {
	ids := []string{}
	for _, callee := range callees {
		if callee.Status == calleeCalling {
			ids = append(ids, callee.UserID)
		}
	}
	return ids
}

// startDirectCall opens a direct channel with a user and calls them.
func (p *Plugin) startDirectCall(callerID, targetID string) (*Meeting, error) {
	if callerID == targetID {
		return nil, newUserError(http.StatusBadRequest, "Нельзя позвонить самому себе.")
	}
	target, appErr := p.API.GetUser(targetID)
	if appErr != nil || target.IsBot || target.DeleteAt != 0 {
		return nil, newUserError(http.StatusNotFound, "Пользователь не найден.")
	}
	if !p.mayMessageDirectly(callerID, targetID) {
		return nil, newUserError(http.StatusForbidden, "Личные сообщения с этим пользователем недоступны.")
	}
	channel, appErr := p.API.GetDirectChannel(callerID, targetID)
	if appErr != nil {
		return nil, fmt.Errorf("direct channel: %w", appErr)
	}
	return p.startMeeting(callerID, createRequest{ChannelID: channel.Id})
}

// mayMessageDirectly applies the server's direct message policy. Opening a
// direct channel through the plugin API skips the check Mattermost makes for
// its own clients, so when messages are limited to team members, the two
// must share a team.
func (p *Plugin) mayMessageDirectly(userID, otherID string) bool {
	config := p.API.GetConfig()
	if config == nil || config.TeamSettings.RestrictDirectMessage == nil || *config.TeamSettings.RestrictDirectMessage != model.DirectMessageTeam {
		return true
	}
	mine, appErr := p.API.GetTeamsForUser(userID)
	if appErr != nil {
		return false
	}
	theirs, appErr := p.API.GetTeamsForUser(otherID)
	if appErr != nil {
		return false
	}
	shared := map[string]bool{}
	for _, team := range mine {
		shared[team.Id] = true
	}
	for _, team := range theirs {
		if shared[team.Id] {
			return true
		}
	}
	return false
}

// personalRoom returns the user's persistent room URL, creating it once.
func (p *Plugin) personalRoom(user *model.User) (string, error) {
	cfg := p.getConfig()
	if cfg.spaceURL() == "" {
		return "", newUserError(http.StatusServiceUnavailable, "Плагин не настроен: укажите адрес пространства в системной консоли.")
	}
	var roomName string
	found, err := p.kvGet(keyPersonal+user.Id, &roomName)
	if err != nil {
		return "", err
	}
	if found {
		return cfg.roomURL(roomName), nil
	}
	roomName = personalRoomName(cfg.RoomPrefix, user.Id)
	if client := p.client(); client != nil {
		params := talk.RoomParams{Title: "Комната " + user.GetDisplayName(model.ShowFullName), EnableLobby: true}
		if identity, ok := p.talkUser(user); ok {
			params.ModeratorKeys = []string{identity.Key}
		}
		ctx, cancel := apiContext()
		defer cancel()
		if _, err := client.PutRoom(ctx, roomName, params); err != nil {
			return "", p.describeAPIError("personal room", err)
		}
	}
	if err := p.kvSet(keyPersonal+user.Id, roomName, 0); err != nil {
		return "", err
	}
	return cfg.roomURL(roomName), nil
}

// canView reports whether a user may see a meeting and what came out of it,
// recordings included: only members of its channel. Read permission is not
// used on purpose, because system admins hold it in every channel without
// being members, and the plugin must not open recordings to them.
func (p *Plugin) canView(userID string, m *Meeting) bool {
	member, appErr := p.API.GetChannelMember(m.ChannelID, userID)
	return appErr == nil && member != nil
}

func (p *Plugin) requireClient() (*talk.Client, error) {
	client := p.client()
	if client == nil {
		return nil, newUserError(http.StatusConflict, "Для этого действия нужен API-ключ Контур.Толка.")
	}
	return client, nil
}

// latestChannelMeeting finds the meeting slash commands in a channel refer to.
func (p *Plugin) latestChannelMeeting(channelID string) (*Meeting, error) {
	m, err := p.getMeetingByIndex(keyChannel + channelID)
	if err == nil && m.isOpen() {
		return m, nil
	}
	if err != nil && !errors.Is(err, errMeetingNotFound) {
		return nil, err
	}
	// The newest meeting may be over while an older one is still running.
	active, err := p.activeMeetings()
	if err != nil {
		return nil, err
	}
	for _, candidate := range active { // Newest first.
		if candidate.ChannelID == channelID && candidate.isOpen() {
			return candidate, nil
		}
	}
	return nil, newUserError(http.StatusNotFound, "В этом канале нет активной встречи. Начните её командой `/talk start`.")
}

// endMeeting ends the conference for everybody.
func (p *Plugin) endMeeting(userID, meetingID string) error {
	m, err := p.controlledMeeting(userID, meetingID, rightRoom)
	if err != nil {
		return err
	}
	client, err := p.requireClient()
	if err != nil {
		return err
	}
	ctx, cancel := apiContext()
	defer cancel()
	if err := client.EndConference(ctx, m.RoomName); err != nil && !talk.IsNotFound(err) {
		return p.describeAPIError("end conference", err)
	}
	p.audit("endMeeting", userID, m.ID)
	p.syncMeetingByID(m.ID)
	return nil
}

// cancelRinging stops ringing but keeps the meeting.
func (p *Plugin) cancelRinging(userID, meetingID string) error {
	m, err := p.getMeeting(meetingID)
	if errors.Is(err, errMeetingNotFound) {
		return newUserError(http.StatusNotFound, "Встреча не найдена.")
	}
	if err != nil {
		return err
	}
	if !p.hasRight(userID, m, rightRing) {
		return errForbidden
	}
	var stopped []string
	updated, changed, err := p.updateMeeting(m.ID, func(m *Meeting) bool {
		stopped = callingUsers(m.Callees)
		if m.Status == statusRinging {
			m.Status = statusWaiting
			m.RingingUntil = time.Now().UTC()
		}
		for i := range m.Callees {
			if m.Callees[i].Status == calleeCalling {
				m.Callees[i].Status = calleeCancelled
			}
		}
		return true
	})
	if err != nil {
		return err
	}
	p.dismissIncoming(m.ID, stopped...)
	if changed {
		p.refreshCard(updated)
	}
	if client := p.client(); client != nil && m.CreatorKey != "" {
		ctx, cancel := apiContext()
		defer cancel()
		if err := client.CancelCall(ctx, m.RoomName, m.CreatorKey); err != nil {
			p.API.LogDebug("Cancel Talk ring failed", "meeting_id", m.ID, "error", err.Error())
		}
	}
	return nil
}

// startRecording starts cloud recording for a running conference. Talk takes
// no access when starting and refuses to change it until the recording is
// finished, so the choice is kept and applied when the recording is ready.
func (p *Plugin) startRecording(userID, meetingID, access string) error {
	if access == "" {
		access = p.getConfig().defaultRecordingAccess()
	}
	if !validAccess(access) {
		return newUserError(http.StatusBadRequest, "Неизвестный вариант доступа к записи.")
	}
	m, err := p.controlledMeeting(userID, meetingID, rightRoom)
	if err != nil {
		return err
	}
	client, err := p.requireClient()
	if err != nil {
		return err
	}
	initiator := m.CreatorKey
	if user, appErr := p.API.GetUser(userID); appErr == nil {
		if identity, ok := p.talkUser(user); ok {
			initiator = identity.Key
		}
	}
	ctx, cancel := apiContext()
	defer cancel()
	key, err := client.StartRecording(ctx, m.RoomName, plainTitle(m.Title), initiator)
	if err != nil {
		// Talk explains refusals in Russian, for example that a room needs a
		// registered participant, and that text is the useful answer here.
		var apiErr *talk.Error
		if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 && apiErr.Status != http.StatusUnauthorized && apiErr.Message != "" {
			p.API.LogInfo("Talk refused to start recording", "meeting_id", m.ID, "status", apiErr.Status, "reason", apiErr.Message)
			return newUserError(http.StatusConflict, "Толк не начал запись: "+apiErr.Message)
		}
		return p.describeAPIError("start recording", err)
	}
	p.audit("startRecording", userID, m.ID)
	updated, changed, err := p.updateMeeting(m.ID, func(m *Meeting) bool {
		m.Recording = true
		if key != "" {
			if m.RecordingChoices == nil {
				m.RecordingChoices = map[string]RecordingChoice{}
			}
			m.RecordingChoices[key] = RecordingChoice{Access: access, Owner: initiator, UserID: userID}
		}
		return true
	})
	if err != nil {
		return err
	}
	if changed {
		p.refreshCard(updated)
	}
	return nil
}

// applyRecordingChoice gives a finished recording the access chosen when it
// was started. The person who chose is told when Talk refuses, since the
// recording then keeps the space's default access.
func (p *Plugin) applyRecordingChoice(client *talk.Client, key string, choice RecordingChoice) {
	var err error
	if choice.Access == accessSelf {
		err = p.restrictToOwner(client, key, choice.Owner)
	} else {
		ctx, cancel := apiContext()
		err = client.SetRecordingLinkAccess(ctx, key, linkScope(choice.Access))
		cancel()
	}
	if appErr := p.API.KVDelete(keyRecAccess + key); appErr != nil {
		p.API.LogWarn("Failed to drop cached recording access", "error", appErr.Error())
	}
	if err == nil {
		return
	}
	p.noteAPIError(err)
	p.API.LogError("Failed to apply recording access", "recording", key, "access", choice.Access, "error", err.Error())
	if choice.UserID != "" {
		p.dmUser(choice.UserID, fmt.Sprintf("Толк не изменил доступ к записи %s. Проверьте, кому она доступна, в Толке.", p.getConfig().recordingURL(key)))
	}
}

// restrictToOwner leaves a recording to the person who started it: link
// access off and every other grant revoked.
func (p *Plugin) restrictToOwner(client *talk.Client, key, owner string) error {
	ctx, cancel := apiContext()
	defer cancel()
	access, err := client.GetRecordingAccess(ctx, key)
	if err != nil {
		return err
	}
	revoke := []string{}
	for i := range access.UserAccesses {
		grant := &access.UserAccesses[i]
		if grant.User.Key != "" && grant.User.Key != owner && grant.RoleID != "none" {
			revoke = append(revoke, grant.User.Key)
		}
	}
	return client.RestrictRecording(ctx, key, "none", revoke)
}

// setPin sets or removes the room PIN.
func (p *Plugin) setPin(userID, meetingID, pin string) error {
	m, err := p.controlledMeeting(userID, meetingID, rightRoom)
	if err != nil {
		return err
	}
	if pin != "" && !validPin(pin) {
		return newUserError(http.StatusBadRequest, "ПИН-код: от 4 до 6 цифр.")
	}
	client, err := p.requireClient()
	if err != nil {
		return err
	}
	ctx, cancel := apiContext()
	defer cancel()
	if err := client.SetRoomPin(ctx, m.RoomName, pin); err != nil {
		return p.describeAPIError("set pin", err)
	}
	return p.saveAndRefresh(m.ID, func(m *Meeting) { m.Pin = pin })
}

func validPin(pin string) bool {
	if len(pin) < 4 || len(pin) > 6 {
		return false
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// setGuests opens the room to guests for some hours; zero closes it.
func (p *Plugin) setGuests(userID, meetingID string, hours int) error {
	m, err := p.controlledMeeting(userID, meetingID, rightRoom)
	if err != nil {
		return err
	}
	client, err := p.requireClient()
	if err != nil {
		return err
	}
	var until *time.Time
	if hours > 0 {
		value := time.Now().UTC().Add(time.Duration(hours) * time.Hour)
		until = &value
	}
	ctx, cancel := apiContext()
	defer cancel()
	if err := client.SetAnonymousAccess(ctx, m.RoomName, until); err != nil {
		return p.describeAPIError("set guests", err)
	}
	return p.saveAndRefresh(m.ID, func(m *Meeting) { m.GuestsUntil = until })
}

// setLobby turns the waiting room on or off.
func (p *Plugin) setLobby(userID, meetingID string, enabled bool) error {
	m, err := p.controlledMeeting(userID, meetingID, rightRoom)
	if err != nil {
		return err
	}
	client, err := p.requireClient()
	if err != nil {
		return err
	}
	m.Lobby = enabled
	ctx, cancel := apiContext()
	defer cancel()
	if _, err := client.PutRoom(ctx, m.RoomName, roomParamsFor(m)); err != nil {
		return p.describeAPIError("set lobby", err)
	}
	return p.saveAndRefresh(m.ID, func(m *Meeting) { m.Lobby = enabled })
}

func (p *Plugin) saveAndRefresh(meetingID string, change func(*Meeting)) error {
	updated, changed, err := p.updateMeeting(meetingID, func(m *Meeting) bool {
		change(m)
		return true
	})
	if err != nil {
		return err
	}
	if changed {
		p.refreshCard(updated)
	}
	return nil
}

func (p *Plugin) audit(event, userID, meetingID string) {
	rec := plugin.MakeAuditRecord(event, model.AuditStatusSuccess)
	rec.Actor.UserId = userID
	rec.AddMeta("meeting_id", meetingID)
	p.API.LogAuditRec(rec)
}

// botReply posts a bot message into the meeting thread.
func (p *Plugin) botReply(m *Meeting, message, postType string, props map[string]any) {
	post := &model.Post{UserId: p.botUserID, ChannelId: m.ChannelID, RootId: m.threadRoot(), Message: message, Type: postType}
	for key, value := range props {
		post.AddProp(key, value)
	}
	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogWarn("Failed to post to meeting thread", "meeting_id", m.ID, "error", appErr.Error())
	}
}

// dmUser sends a direct message from the bot.
func (p *Plugin) dmUser(userID, message string) {
	channel, appErr := p.API.GetDirectChannel(userID, p.botUserID)
	if appErr != nil {
		p.API.LogWarn("Failed to open bot DM", "user_id", userID, "error", appErr.Error())
		return
	}
	if _, appErr := p.API.CreatePost(&model.Post{UserId: p.botUserID, ChannelId: channel.Id, Message: message}); appErr != nil {
		p.API.LogWarn("Failed to send bot DM", "user_id", userID, "error", appErr.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
