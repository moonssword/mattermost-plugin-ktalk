package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/mattermost/mattermost/server/public/model"
)

// Naming schemes for new room keys.
const (
	schemeWords   = "words"
	schemeUUID    = "uuid"
	schemeContext = "context"
	schemeAsk     = "ask"
)

// Visual styles of the plugin's own interface.
const (
	styleCurrent = "current"
	styleCards   = "cards"
	styleCompact = "compact"
)

// configuration mirrors settings_schema in plugin.json.
type configuration struct {
	SpaceURL             string
	APIKey               string
	EnableWebhooks       bool
	NamingScheme         string
	RoomPrefix           string
	PublicLobby          bool
	PublicGuestHours     int
	MuteAboveMembers     int
	PostRecordings       bool
	PostSummaries        bool
	RecordingLinkAccess  string
	EnableCalls          bool
	AllowPersonalKeys    bool
	AllowGuests          bool
	RequireVerifiedEmail bool
	ReminderMinutes      int
	CalendarPersonalOnly bool
	SetMeetingStatus     bool
	RingInTalk           bool
	CardStyle            string
	TestMode             bool
	TestModeUsers        string
	TestModeAdmins       bool

	testers map[string]bool
}

// spaceURL returns the normalized space address, "" when not configured.
func (c *configuration) spaceURL() string {
	return strings.TrimRight(strings.TrimSpace(c.SpaceURL), "/")
}

func (c *configuration) roomURL(roomName string) string {
	return c.spaceURL() + "/" + roomName
}

func (c *configuration) recordingURL(key string) string {
	return c.spaceURL() + "/recordings/" + url.PathEscape(key)
}

func (c *configuration) artifactsURL(conferenceKey string) string {
	return c.spaceURL() + "/content/artifacts?cid=" + url.QueryEscape(conferenceKey)
}

// validate rejects settings the plugin cannot work with.
func (c *configuration) validate() error {
	raw := c.spaceURL()
	if raw == "" {
		return nil // Not configured yet: the plugin stays up and explains this.
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return fmt.Errorf("SpaceURL must be an address like https://company.ktalk.ru, got %q", raw)
	}
	if parsed.Path != "" || parsed.RawQuery != "" {
		return errors.New("SpaceURL must not contain a path or query")
	}
	return nil
}

func (c *configuration) applyDefaults() {
	switch c.NamingScheme {
	case schemeWords, schemeUUID, schemeContext, schemeAsk:
	default:
		c.NamingScheme = schemeWords
	}
	switch c.RecordingLinkAccess {
	case "domain", "global", "none":
	default:
		c.RecordingLinkAccess = ""
	}
	switch c.CardStyle {
	case styleCurrent, styleCards, styleCompact:
	default:
		c.CardStyle = styleCurrent
	}
	c.testers = parseUsernames(c.TestModeUsers)
	c.RoomPrefix = sanitizeRoomPart(c.RoomPrefix)
	if c.ReminderMinutes <= 0 {
		c.ReminderMinutes = 10
	}
	if c.MuteAboveMembers <= 0 {
		c.MuteAboveMembers = 15
	}
}

// parseUsernames reads a list separated by commas, spaces or new lines. A
// leading @ is accepted, since that is how people write usernames in chat.
func parseUsernames(raw string) map[string]bool {
	users := map[string]bool{}
	for _, name := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || unicode.IsSpace(r) }) {
		name = strings.ToLower(strings.TrimPrefix(name, "@"))
		if name != "" {
			users[name] = true
		}
	}
	return users
}

// allowsUser applies test mode: while it is on, only the listed testers use
// the plugin. System administrators join them only when the settings say so,
// since a large organisation may have many of them. The System Console works
// for administrators either way.
func (c *configuration) allowsUser(user *model.User) bool {
	if !c.TestMode {
		return true
	}
	if user == nil {
		return false
	}
	return c.testers[strings.ToLower(user.Username)] || (c.TestModeAdmins && user.IsSystemAdmin())
}

func generateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
