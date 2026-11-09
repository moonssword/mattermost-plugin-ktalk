package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"regexp"
	"strings"
)

const maxRoomNameLength = 64

// The Talk API accepts only lowercase latin letters and digits in room keys.
var (
	roomNamePattern = regexp.MustCompile(`^[a-z0-9]{3,64}$`)
	notRoomChars    = regexp.MustCompile(`[^a-z0-9]+`)
)

var (
	roomAdjectives = []string{"quiet", "bright", "swift", "calm", "brave", "clever", "gentle", "merry", "noble", "sunny", "warm", "wise", "bold", "kind", "lucky", "steady"}
	roomNouns      = []string{"deer", "otter", "falcon", "lynx", "heron", "fox", "badger", "swan", "marten", "crane", "hare", "owl", "seal", "wolf", "bison", "raven"}
	roomVerbs      = []string{"dance", "sing", "gather", "travel", "explore", "wander", "build", "plan", "meet", "learn", "share", "think"}
)

func validRoomName(name string) bool { return roomNamePattern.MatchString(name) }

func sanitizeRoomPart(s string) string {
	return notRoomChars.ReplaceAllString(strings.ToLower(s), "")
}

func randomInt(limit int) int {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		panic(err) // crypto/rand failing leaves nothing safe to do.
	}
	return int(n.Int64())
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

// generateRoomName builds a room key for a scheme. teamName and channelName
// are Mattermost URL names, used by the context scheme only.
func generateRoomName(scheme, prefix, teamName, channelName string) string {
	var body string
	switch scheme {
	case schemeUUID:
		body = randomHex(16)
	case schemeContext:
		body = sanitizeRoomPart(teamName) + sanitizeRoomPart(channelName) + randomHex(2)
	default:
		body = roomAdjectives[randomInt(len(roomAdjectives))] +
			roomNouns[randomInt(len(roomNouns))] +
			roomVerbs[randomInt(len(roomVerbs))] +
			randomHex(1)
	}
	name := prefix + body
	if len(name) > maxRoomNameLength {
		// Keep the random tail, which is what makes the key unique.
		name = name[:maxRoomNameLength-4] + name[len(name)-4:]
	}
	return name
}

// personalRoomName derives a stable room key for a user.
func personalRoomName(prefix, userID string) string {
	sum := sha256.Sum256([]byte("personal:" + userID))
	return prefix + "p" + hex.EncodeToString(sum[:8])
}
