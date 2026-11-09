package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

const (
	userMapTTL      = 24 * time.Hour
	userMapMissTTL  = 10 * time.Minute
	userStatusTTL   = 30 * time.Second
	emailBatchSize  = 50
	maxCallees      = 20
	prefNaming      = "naming"
	prefOpenMode    = "open_mode"
	prefNotifyRec   = "notify_recording"
	prefAcceptCalls = "accept_calls"
	prefStyle       = "style"
)

// talkIdentity caches the link between a Mattermost and a Talk user.
type talkIdentity struct {
	Email string `json:"email"`
	Key   string `json:"key"`
	Name  string `json:"name"`
}

// emailProblem explains why a user cannot act under their address in Talk, or
// returns nil. Mattermost clears verification when an address changes, and a
// server without mail settings cannot verify by letter, so the text names
// both ways out.
func (p *Plugin) emailProblem(user *model.User) *userError {
	if user == nil || user.Email == "" {
		return newCodedError(http.StatusBadRequest, codeEmailUnverified, "В профиле Mattermost не указан адрес почты.")
	}
	if p.talkEmail(user) != "" {
		return nil
	}
	return newCodedError(http.StatusBadRequest, codeEmailUnverified,
		"Адрес почты в профиле Mattermost не подтверждён, поэтому плагин не может связать вас с аккаунтом Толка. "+
			"Подтвердите адрес по ссылке из письма Mattermost. Если письма не приходят, попросите администратора "+
			"подтвердить адрес командой mmctl user verify.")
}

// talkEmail returns the address a user may act under in Talk. An unverified
// address could be set to a colleague's and grant access to their calendar.
func (p *Plugin) talkEmail(user *model.User) string {
	if user == nil || user.IsBot || user.DeleteAt != 0 || user.Email == "" {
		return ""
	}
	if p.getConfig().RequireVerifiedEmail && !user.EmailVerified {
		return ""
	}
	return strings.ToLower(user.Email)
}

// resolveTalkUsers maps Mattermost users to Talk users by email, with a cache.
// Users absent from Talk are simply missing from the result.
func (p *Plugin) resolveTalkUsers(users []*model.User) (map[string]talkIdentity, error) {
	result := make(map[string]talkIdentity, len(users))
	client := p.client()
	if client == nil {
		return result, nil
	}

	pending := map[string]string{}
	for _, user := range users {
		email := p.talkEmail(user)
		if email == "" {
			continue
		}
		var cached talkIdentity
		found, err := p.kvGet(keyUserMap+user.Id, &cached)
		if err != nil {
			p.API.LogWarn("User cache read failed", "error", err.Error())
		}
		if found && cached.Email == email {
			if cached.Key != "" {
				result[user.Id] = cached
			}
			continue
		}
		pending[email] = user.Id
	}

	emails := make([]string, 0, len(pending))
	for email := range pending {
		emails = append(emails, email)
	}
	for len(emails) > 0 {
		size := min(emailBatchSize, len(emails))
		batch := emails[:size]
		emails = emails[size:]

		ctx, cancel := apiContext()
		found, err := client.FindUsersByEmail(ctx, batch, false)
		cancel()
		if err != nil {
			p.noteAPIError(err)
			return result, err
		}
		seen := map[string]bool{}
		for i := range found {
			email := strings.ToLower(found[i].Email)
			userID, ok := pending[email]
			if !ok || found[i].Disabled {
				continue
			}
			identity := talkIdentity{Email: email, Key: found[i].Key, Name: found[i].DisplayName()}
			result[userID] = identity
			seen[email] = true
			if err := p.kvSet(keyUserMap+userID, identity, userMapTTL); err != nil {
				p.API.LogWarn("User cache write failed", "error", err.Error())
			}
		}
		for _, email := range batch {
			if seen[email] {
				continue
			}
			if err := p.kvSet(keyUserMap+pending[email], talkIdentity{Email: email}, userMapMissTTL); err != nil {
				p.API.LogWarn("User cache write failed", "error", err.Error())
			}
		}
	}
	return result, nil
}

func (p *Plugin) talkUser(user *model.User) (talkIdentity, bool) {
	found, err := p.resolveTalkUsers([]*model.User{user})
	if err != nil {
		p.API.LogWarn("Talk user lookup failed", "user_id", user.Id, "error", err.Error())
	}
	identity, ok := found[user.Id]
	return identity, ok
}

// talkStatus returns inMeeting, doNotDisturb, online, unknown or "" (no user).
func (p *Plugin) talkStatus(user *model.User) (string, error) {
	email := p.talkEmail(user)
	client := p.client()
	if email == "" || client == nil {
		return "", nil
	}
	var cached string
	if found, _ := p.kvGet(keyStatus+user.Id, &cached); found {
		return cached, nil
	}
	ctx, cancel := apiContext()
	defer cancel()
	found, err := client.FindUsersByEmail(ctx, []string{email}, true)
	if err != nil {
		p.noteAPIError(err)
		return "", err
	}
	status := ""
	for i := range found {
		if strings.EqualFold(found[i].Email, email) {
			status = found[i].Status
			if status == "" {
				status = "unknown"
			}
		}
	}
	if err := p.kvSet(keyStatus+user.Id, status, userStatusTTL); err != nil {
		p.API.LogWarn("Status cache write failed", "error", err.Error())
	}
	return status, nil
}

func (p *Plugin) userPref(userID, name, fallback string) string {
	pref, appErr := p.API.GetPreferenceForUser(userID, prefCategory, name)
	if appErr != nil || pref.Value == "" {
		return fallback
	}
	return pref.Value
}

func (p *Plugin) setUserPref(userID, name, value string) error {
	appErr := p.API.UpdatePreferencesForUser(userID, []model.Preference{{UserId: userID, Category: prefCategory, Name: name, Value: value}})
	if appErr != nil {
		return appErr
	}
	return nil
}

// personalClient returns a client with the user's personal key, nil when the
// user has none or personal keys are off.
func (p *Plugin) personalClient(userID string) *talk.Client {
	base := p.client()
	if base == nil || !p.getConfig().AllowPersonalKeys {
		return nil
	}
	var stored personalKey
	found, err := p.kvGet(keyPAT+userID, &stored)
	if err != nil || !found {
		return nil
	}
	token, err := p.decrypt(stored.Token)
	if err != nil {
		p.API.LogWarn("Failed to decrypt personal key", "user_id", userID, "error", err.Error())
		return nil
	}
	return base.WithPersonalToken(token)
}

type personalKey struct {
	Token string `json:"token"`
	// Name is the Talk user the key belongs to, shown in the interface.
	Name        string    `json:"name,omitempty"`
	ConnectedAt time.Time `json:"connected_at,omitempty"`
	// ExpiresAt is kept for keys stored by earlier versions.
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// connectPersonalKey checks a personal key against Talk and stores it
// encrypted. The key must belong to the same Talk user the Mattermost account
// maps to, or meetings would be planned under someone else's name.
func (p *Plugin) connectPersonalKey(userID, token string) (personalKey, error) {
	base := p.client()
	if base == nil {
		return personalKey{}, errNotConfigured
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return personalKey{}, newUserError(http.StatusBadRequest, "Вставьте ключ.")
	}
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return personalKey{}, appErr
	}
	if problem := p.emailProblem(user); problem != nil {
		return personalKey{}, problem
	}
	client := base.WithPersonalToken(token)
	ctx, cancel := apiContext()
	defer cancel()
	profile, err := client.GetProfile(ctx)
	if err != nil {
		return personalKey{}, err
	}
	if identity, ok := p.talkUser(user); ok && !sameName(identity.Name, profile.Name()) {
		return personalKey{}, newUserError(http.StatusBadRequest, "Это ключ другого пользователя Толка ("+profile.Name()+"). Выпустите ключ в своей учётной записи.")
	}
	now := time.Now().UTC()
	if _, err := client.ListEvents(ctx, "", now, now.Add(24*time.Hour)); err != nil && !talk.IsNotFound(err) {
		return personalKey{}, err
	}
	encrypted, err := p.encrypt(token)
	if err != nil {
		return personalKey{}, err
	}
	stored := personalKey{Token: encrypted, Name: profile.Name(), ConnectedAt: now}
	if err := p.kvSet(keyPAT+userID, stored, 0); err != nil {
		return personalKey{}, err
	}
	return stored, nil
}

// sameName compares two spellings of a person's name. Talk writes it as
// "Name Surname" in one place and "Surname Name" in another.
func sameName(a, b string) bool {
	words := func(s string) map[string]bool {
		out := map[string]bool{}
		for _, w := range strings.Fields(strings.ToLower(s)) {
			out[w] = true
		}
		return out
	}
	x, y := words(a), words(b)
	if len(x) == 0 || len(y) == 0 {
		return true
	}
	for w := range y {
		if !x[w] {
			return false
		}
	}
	return true
}

// disconnectPersonalKey forgets a user's personal key.
func (p *Plugin) disconnectPersonalKey(userID string) error {
	if appErr := p.API.KVDelete(keyPAT + userID); appErr != nil {
		return appErr
	}
	return nil
}

// personalKeyStatus is what the interface shows about a user's key.
func (p *Plugin) personalKeyStatus(userID string) map[string]any {
	var stored personalKey
	found, _ := p.kvGet(keyPAT+userID, &stored)
	status := map[string]any{"allowed": p.getConfig().AllowPersonalKeys && p.client() != nil, "connected": found}
	if found {
		status["name"] = stored.Name
		if !stored.ConnectedAt.IsZero() {
			status["connected_at"] = stored.ConnectedAt
		}
	}
	return status
}

var errPersonalKeyRevoked = newCodedError(http.StatusConflict, codePersonalKey, "Личный ключ Толка больше не действует: он истёк или отозван. Подключите новый ключ в настройках Контур.Толка.")

// calendarError maps a calendar failure. When Talk refuses a personal key,
// the key is dropped and its owner told, so the next attempt falls back to
// the space key instead of failing the same way again.
func (p *Plugin) calendarError(client *talk.Client, ownerID, action string, err error) *userError {
	if client != nil && client.Personal() && ownerID != "" && talk.IsStatus(err, http.StatusUnauthorized) {
		if dropErr := p.disconnectPersonalKey(ownerID); dropErr == nil {
			p.dmUser(ownerID, "Личный ключ Толка больше не действует: он истёк или отозван. Плагин отключил его. Подключите новый ключ в «Настройки → Контур.Толк» или на вкладке «Запланированные».")
		}
		return errPersonalKeyRevoked
	}
	return p.describeAPIError(action, err)
}

// personalKeyMessage explains why Talk refused a personal key.
func personalKeyMessage(err error) string {
	var userErr *userError
	switch {
	case errors.As(err, &userErr):
		return userErr.message
	case talk.IsStatus(err, http.StatusUnauthorized):
		return "Толк не принял этот ключ: он неверный, отозван или истёк. Выпустите новый ключ в Толке."
	case talk.IsStatus(err, http.StatusForbidden):
		return "Толк не дал этому ключу доступ к календарю. Проверьте, что ключ выпущен в этом пространстве Толка."
	}
	return "Не удалось проверить ключ в Толке. Попробуйте ещё раз позже."
}

// personalKeyConnected reports a connected key.
func personalKeyConnected(key personalKey) string {
	who := ""
	if key.Name != "" {
		who = " Ключ выпущен пользователем Толка " + key.Name + "."
	}
	return "Личный ключ Толка подключён: встречи, которые вы планируете, создаются от вашего имени, а на приглашения можно отвечать прямо в Mattermost." + who
}
