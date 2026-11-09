// Command plugin is the server side of the Kontur Talk plugin for Mattermost.
//
// It talks to one Kontur Talk space through the typed client in package talk,
// keeps every meeting it created in the plugin KV store, and keeps the meeting
// cards in channels up to date by polling the space, or sooner when Talk
// delivers a webhook.
package main
