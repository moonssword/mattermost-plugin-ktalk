package main

import (
	"net/http"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

const participantSearchLimit = 20

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
		if req.TeamId == "" || !p.API.HasPermissionToTeam(userID, req.TeamId, model.PermissionViewTeam) {
			return errForbidden
		}
		users, appErr = p.API.SearchUsers(&model.UserSearch{Term: query, TeamId: req.TeamId, Limit: participantSearchLimit})
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
