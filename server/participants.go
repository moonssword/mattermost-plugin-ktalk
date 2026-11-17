package main

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	participantSearchLimit = 20
	rosterTTL              = time.Minute
	rosterPage             = 200
)

// teamRoster is a team's active people, kept for a minute so typing a name
// does not list the team on every keystroke.
type teamRoster struct {
	users []*model.User
	at    time.Time
}

// searchTeamUsers finds people of the teams by username, nickname, first and
// last name. The plugin API search of Mattermost looks only at usernames and
// nicknames, so "Иван" or "Петров" found nobody. Names are searched only when
// the server shows full names, as in the standard user picker.
func (p *Plugin) searchTeamUsers(teamIDs []string, query string) ([]*model.User, *model.AppError) {
	users := []*model.User{}
	listed := map[string]bool{}
	for _, teamID := range teamIDs {
		roster, appErr := p.teamRoster(teamID)
		if appErr != nil {
			return nil, appErr
		}
		for _, user := range roster {
			if !listed[user.Id] {
				listed[user.Id] = true
				users = append(users, user)
			}
		}
	}
	fullNames := true
	if cfg := p.API.GetConfig(); cfg != nil && cfg.PrivacySettings.ShowFullName != nil {
		fullNames = *cfg.PrivacySettings.ShowFullName
	}
	terms := strings.Fields(searchFold(strings.TrimPrefix(strings.TrimSpace(query), "@")))
	found := []*model.User{}
	for _, user := range users {
		if userMatches(user, terms, fullNames) {
			found = append(found, user)
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		return searchFold(participantValue(found[i])) < searchFold(participantValue(found[j]))
	})
	if len(found) > participantSearchLimit {
		found = found[:participantSearchLimit]
	}
	return found, nil
}

func (p *Plugin) teamRoster(teamID string) ([]*model.User, *model.AppError) {
	p.rosterLock.Lock()
	defer p.rosterLock.Unlock()
	if cached, ok := p.rosters[teamID]; ok && time.Since(cached.at) < rosterTTL {
		return cached.users, nil
	}
	users := []*model.User{}
	for page := 0; ; page++ {
		batch, appErr := p.API.GetUsersInTeam(teamID, page, rosterPage)
		if appErr != nil {
			return nil, appErr
		}
		for _, user := range batch {
			if !user.IsBot && user.DeleteAt == 0 {
				users = append(users, user)
			}
		}
		if len(batch) < rosterPage {
			break
		}
	}
	if p.rosters == nil {
		p.rosters = map[string]teamRoster{}
	}
	p.rosters[teamID] = teamRoster{users: users, at: time.Now()}
	return users, nil
}

// userMatches wants every term to start one of the user's words, in any
// order: "петров ив" finds Иван Петров. A username counts whole and by its
// parts, so "petrov" finds ivan.petrov.
func userMatches(user *model.User, terms []string, fullNames bool) bool {
	if len(terms) == 0 {
		return false
	}
	username := searchFold(user.Username)
	words := append([]string{username}, strings.FieldsFunc(username, func(r rune) bool { return r == '.' || r == '_' || r == '-' })...)
	words = append(words, strings.Fields(searchFold(user.Nickname))...)
	if fullNames {
		words = append(words, strings.Fields(searchFold(user.FirstName+" "+user.LastName))...)
	}
	for _, term := range terms {
		matched := false
		for _, word := range words {
			if strings.HasPrefix(word, term) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// searchFold makes a search case-blind and lets "е" find "ё".
func searchFold(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "ё", "е")
}

// participantValue is how the schedule form names a user: "Иван Петров
// (@ivan.petrov)". The form shows a preselected value as it is, so the value
// itself has to read well, and the username in it finds the user again.
// Commas would split the value, so the name loses them.
func participantValue(user *model.User) string {
	name := strings.Join(strings.Fields(strings.ReplaceAll(user.GetDisplayName(model.ShowFullName), ",", " ")), " ")
	if name == "" || name == user.Username {
		return "@" + user.Username
	}
	return name + " (@" + user.Username + ")"
}

// participantUsername takes the username back out of a form value.
func participantUsername(value string) string {
	value = strings.TrimSpace(value)
	if at := strings.LastIndex(value, "@"); at >= 0 {
		return strings.TrimSuffix(value[at+1:], ")")
	}
	return ""
}

// participantValues names the invited users for the edit form.
func (p *Plugin) participantValues(userIDs []string) string {
	if len(userIDs) == 0 {
		return ""
	}
	users, appErr := p.API.GetUsersByIds(userIDs)
	if appErr != nil {
		p.API.LogWarn("Participants unavailable for the form", "error", appErr.Error())
		return ""
	}
	byID := make(map[string]*model.User, len(users))
	for _, user := range users {
		byID[user.Id] = user
	}
	values := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if user, ok := byID[id]; ok {
			values = append(values, participantValue(user))
		}
	}
	return strings.Join(values, ",")
}

// participantIDs turns form values back into user ids. A bare id is accepted
// too. The first value that matches nobody is returned to show to the user.
func (p *Plugin) participantIDs(values []string) ([]string, string) {
	ids := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		id := ""
		if model.IsValidId(value) {
			id = value
		} else if username := participantUsername(value); username != "" {
			if user, appErr := p.API.GetUserByUsername(username); appErr == nil {
				id = user.Id
			}
		}
		if id == "" {
			return nil, value
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, ""
}

// dialogUsers answers the participant search of the schedule form. Without a
// query it offers the members of the channel the form was opened in.
func (p *Plugin) dialogUsers(w http.ResponseWriter, r *http.Request, userID string) error {
	req, err := dialogRequest(r, userID)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(stringField(req.Submission, "query"))
	var users []*model.User
	var appErr *model.AppError
	if query == "" && req.ChannelId != "" {
		if !p.API.HasPermissionToChannel(userID, req.ChannelId, model.PermissionReadChannel) {
			return errForbidden
		}
		users, appErr = p.API.GetUsersInChannel(req.ChannelId, "username", 0, participantSearchLimit)
	} else {
		// The same reach as the standard user picker: the current team, and
		// only for its members. The plugin API does not check that itself.
		// A direct or group chat belongs to no team, and the form opened there
		// comes without one: it searches all the teams of the user.
		var teamIDs []string
		if req.TeamId != "" {
			if !p.API.HasPermissionToTeam(userID, req.TeamId, model.PermissionViewTeam) {
				return errForbidden
			}
			teamIDs = []string{req.TeamId}
		} else {
			teams, teamsErr := p.API.GetTeamsForUser(userID)
			if teamsErr != nil {
				return teamsErr
			}
			for _, team := range teams {
				teamIDs = append(teamIDs, team.Id)
			}
		}
		users, appErr = p.searchTeamUsers(teamIDs, query)
	}
	if appErr != nil {
		return appErr
	}
	items := make([]model.DialogSelectOption, 0, len(users))
	for _, user := range users {
		if user.IsBot || user.DeleteAt != 0 {
			continue
		}
		value := participantValue(user)
		items = append(items, model.DialogSelectOption{Text: value, Value: value})
	}
	writeJSON(w, http.StatusOK, model.LookupDialogResponse{Items: items})
	return nil
}
