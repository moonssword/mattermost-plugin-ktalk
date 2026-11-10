package main

import (
	"net/http"
	"strings"
)

// keySeen marks a user of the plugin: someone who opened its panel, started
// a meeting or connected a key. It feeds the count in the System Console.
const keySeen = "seen:"

// markSeen records a user of the plugin once.
func (p *Plugin) markSeen(userID string) {
	if !p.userAllowed(userID) {
		return
	}
	var seen int
	if found, err := p.kvGet(keySeen+userID, &seen); err != nil || found {
		return
	}
	if _, err := p.kvClaim(keySeen+userID, 0); err != nil {
		p.API.LogWarn("Failed to mark a plugin user", "user_id", userID, "error", err.Error())
	}
}

// apiSeen is called when the panel opens.
func (p *Plugin) apiSeen(w http.ResponseWriter, _ *http.Request, userID string) error {
	p.markSeen(userID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}

// keyUsage counts the users of the plugin and those of them with a personal
// key, so administrators can see how far the move to personal keys has come.
func (p *Plugin) keyUsage() (withKey, users int, err error) {
	const perPage = 1000
	seen := map[string]bool{}
	for page := 0; ; page++ {
		keys, appErr := p.API.KVList(page, perPage)
		if appErr != nil {
			return 0, 0, appErr
		}
		for _, key := range keys {
			switch {
			case strings.HasPrefix(key, keyPAT):
				withKey++
				seen[strings.TrimPrefix(key, keyPAT)] = true
			case strings.HasPrefix(key, keySeen):
				seen[strings.TrimPrefix(key, keySeen)] = true
			}
		}
		if len(keys) < perPage {
			return withKey, len(seen), nil
		}
	}
}
