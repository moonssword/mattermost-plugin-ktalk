package main

import (
	_ "time/tzdata" // User time zones must resolve in minimal server images.

	"github.com/mattermost/mattermost/server/public/plugin"
)

func main() {
	plugin.ClientMain(&Plugin{})
}
