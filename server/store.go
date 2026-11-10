package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	casRetries      = 8
	finishedKeepFor = 90 * 24 * time.Hour

	keyMeeting   = "meeting:"
	keyRoom      = "room:"
	keyConf      = "conf:"
	keyRec       = "rec:"
	keyRecAccess = "racc:"
	keyChannel   = "chan:"
	keyPersonal  = "personal:"
	keyPAT       = "pat:"
	keyUserMap   = "umap:"
	keyStatus    = "ustat:"
	keyEvent     = "event:"
	keyAlert     = "alert:"
	keyActive    = "active"
	keyWebhook   = "webhook"
	keyActivate  = "webhook_activation"
	keySecret    = "secret:"
)

var errMeetingNotFound = errors.New("meeting not found")

func (p *Plugin) kvGet(key string, out any) (bool, error) {
	data, appErr := p.API.KVGet(key)
	if appErr != nil {
		return false, fmt.Errorf("kv get %s: %w", key, appErr)
	}
	if data == nil {
		return false, nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return false, fmt.Errorf("kv decode %s: %w", key, err)
	}
	return true, nil
}

func (p *Plugin) kvSet(key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("kv encode %s: %w", key, err)
	}
	options := model.PluginKVSetOptions{ExpireInSeconds: int64(ttl / time.Second)}
	if _, appErr := p.API.KVSetWithOptions(key, data, options); appErr != nil {
		return fmt.Errorf("kv set %s: %w", key, appErr)
	}
	return nil
}

// kvClaim atomically creates a key, reporting whether this call created it.
func (p *Plugin) kvClaim(key string, ttl time.Duration) (bool, error) {
	options := model.PluginKVSetOptions{Atomic: true, OldValue: nil, ExpireInSeconds: int64(ttl / time.Second)}
	ok, appErr := p.API.KVSetWithOptions(key, []byte("1"), options)
	if appErr != nil {
		return false, fmt.Errorf("kv claim %s: %w", key, appErr)
	}
	return ok, nil
}

func (p *Plugin) getMeeting(id string) (*Meeting, error) {
	var m Meeting
	found, err := p.kvGet(keyMeeting+id, &m)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errMeetingNotFound
	}
	return &m, nil
}

func (p *Plugin) getMeetingByIndex(key string) (*Meeting, error) {
	var id string
	found, err := p.kvGet(key, &id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errMeetingNotFound
	}
	return p.getMeeting(id)
}

// saveNewMeeting stores a new meeting and its lookups.
func (p *Plugin) saveNewMeeting(m *Meeting) error {
	if err := p.kvSet(keyMeeting+m.ID, m, 0); err != nil {
		return err
	}
	if err := p.kvSet(keyRoom+m.RoomName, m.ID, 0); err != nil {
		return err
	}
	if err := p.kvSet(keyChannel+m.ChannelID, m.ID, 0); err != nil {
		return err
	}
	if m.Kind == kindPersonal {
		return nil // Personal rooms stay open, so they are never polled.
	}
	return p.addActive(m.ID)
}

// updateMeeting applies a change with compare-and-set, so concurrent webhook
// and polling work on any node cannot lose updates. fn returns false to skip
// saving. The result is the stored meeting and whether it changed.
func (p *Plugin) updateMeeting(id string, fn func(*Meeting) bool) (*Meeting, bool, error) {
	key := keyMeeting + id
	for range casRetries {
		oldData, appErr := p.API.KVGet(key)
		if appErr != nil {
			return nil, false, fmt.Errorf("kv get %s: %w", key, appErr)
		}
		if oldData == nil {
			return nil, false, errMeetingNotFound
		}
		m := &Meeting{}
		if err := json.Unmarshal(oldData, m); err != nil {
			return nil, false, fmt.Errorf("kv decode %s: %w", key, err)
		}
		if !fn(m) {
			return m, false, nil
		}
		newData, err := json.Marshal(m)
		if err != nil {
			return nil, false, fmt.Errorf("kv encode %s: %w", key, err)
		}
		if string(newData) == string(oldData) {
			return m, false, nil
		}
		options := model.PluginKVSetOptions{Atomic: true, OldValue: oldData}
		if m.Status == statusEnded || m.Status == statusCancelled {
			if artifactsSettled(m, time.Now()) || m.Status == statusCancelled {
				options.ExpireInSeconds = int64(finishedKeepFor / time.Second)
			}
		}
		ok, appErr := p.API.KVSetWithOptions(key, newData, options)
		if appErr != nil {
			return nil, false, fmt.Errorf("kv set %s: %w", key, appErr)
		}
		if ok {
			return m, true, nil
		}
	}
	return nil, false, fmt.Errorf("meeting %s: too many concurrent updates", id)
}

func (p *Plugin) mutateActive(fn func([]string) []string) error {
	return p.mutateList(keyActive, fn)
}

// mutateList changes a stored list of ids with compare-and-set.
func (p *Plugin) mutateList(key string, fn func([]string) []string) error {
	for range casRetries {
		oldData, appErr := p.API.KVGet(key)
		if appErr != nil {
			return fmt.Errorf("kv get active: %w", appErr)
		}
		var ids []string
		if oldData != nil {
			if err := json.Unmarshal(oldData, &ids); err != nil {
				return fmt.Errorf("kv decode %s: %w", key, err)
			}
		}
		newData, err := json.Marshal(fn(ids))
		if err != nil {
			return err
		}
		options := model.PluginKVSetOptions{Atomic: true, OldValue: oldData}
		ok, appErr := p.API.KVSetWithOptions(key, newData, options)
		if appErr != nil {
			return fmt.Errorf("kv set %s: %w", key, appErr)
		}
		if ok {
			return nil
		}
	}
	return fmt.Errorf("%s: too many concurrent updates", key)
}

func (p *Plugin) addActive(id string) error {
	return p.mutateActive(func(ids []string) []string {
		for _, existing := range ids {
			if existing == id {
				return ids
			}
		}
		return append(ids, id)
	})
}

func (p *Plugin) removeActive(id string) error {
	return p.mutateActive(func(ids []string) []string {
		kept := ids[:0]
		for _, existing := range ids {
			if existing != id {
				kept = append(kept, existing)
			}
		}
		return kept
	})
}

func (p *Plugin) activeMeetings() ([]*Meeting, error) {
	var ids []string
	if _, err := p.kvGet(keyActive, &ids); err != nil {
		return nil, err
	}
	meetings := make([]*Meeting, 0, len(ids))
	for _, id := range ids {
		m, err := p.getMeeting(id)
		if errors.Is(err, errMeetingNotFound) {
			if rmErr := p.removeActive(id); rmErr != nil {
				p.API.LogWarn("Failed to drop stale active meeting", "meeting_id", id, "error", rmErr.Error())
			}
			continue
		}
		if err != nil {
			p.API.LogWarn("Failed to load active meeting", "meeting_id", id, "error", err.Error())
			continue
		}
		meetings = append(meetings, m)
	}
	sort.Slice(meetings, func(i, j int) bool { return meetings[i].CreatedAt.After(meetings[j].CreatedAt) })
	return meetings, nil
}
