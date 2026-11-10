package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// A Tuesday morning in Yekaterinburg, which is UTC+5.
var (
	ekb, _   = time.LoadLocation("Asia/Yekaterinburg")
	tuesday  = time.Date(2026, 11, 10, 12, 0, 0, 0, ekb)
	lateNigh = time.Date(2026, 11, 10, 2, 0, 0, 0, ekb) // Still Monday in UTC.
)

func TestRepeatTextReadsLikeTalk(t *testing.T) {
	cases := map[string]RepeatRule{
		"Каждую неделю по вторникам до 10.12.2026":                     {Freq: repeatWeekly, Interval: 1, Days: []string{"tuesday"}, Until: "2026-12-10"},
		"Каждую неделю по будням до 10.12.2026":                        {Freq: repeatWeekly, Interval: 1, Days: slices.Clone(workweek), Until: "2026-12-10"},
		"Каждые 2 недели по вторникам и четвергам, 6 встреч":           {Freq: repeatWeekly, Interval: 2, Days: []string{"tuesday", "thursday"}, Count: 6},
		"Каждую неделю по понедельникам, средам и пятницам, 2 встречи": {Freq: repeatWeekly, Interval: 1, Days: []string{"monday", "wednesday", "friday"}, Count: 2},
		"Каждый день, 10 встреч":                                       {Freq: repeatDaily, Interval: 1, Count: 10},
		"Каждые 3 дня до 01.01.2027":                                   {Freq: repeatDaily, Interval: 3, Until: "2027-01-01"},
		"Каждые 5 дней, 21 встреча":                                    {Freq: repeatDaily, Interval: 5, Count: 21},
		"Каждый 21 день, 3 встречи":                                    {Freq: repeatDaily, Interval: 21, Count: 3},
		"Каждый месяц, 10-го числа до 10.05.2027":                      {Freq: repeatMonthly, Interval: 1, DayOfMonth: 10, Until: "2027-05-10"},
		"Каждые 11 месяцев, 1-го числа, 2 встречи":                     {Freq: repeatMonthly, Interval: 11, DayOfMonth: 1, Count: 2},
	}
	for want, rule := range cases {
		if got := rule.Text(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestPresetsFollowTheStartInTheOrganizersZone(t *testing.T) {
	if got := presetRule(repeatWeekly, tuesday).Days; !slices.Equal(got, []string{"tuesday"}) {
		t.Errorf("weekly: %v", got)
	}
	// 02:00 on Tuesday in Yekaterinburg is Monday in UTC; Tuesday is meant.
	if got := presetRule(repeatWeekly, lateNigh).Days; !slices.Equal(got, []string{"tuesday"}) {
		t.Errorf("weekly late at night: %v", got)
	}
	if got := presetRule(repeatMonthly, tuesday).DayOfMonth; got != 10 {
		t.Errorf("monthly: %d", got)
	}
	for _, choice := range []string{repeatWeekdays, repeatDaily, repeatWeekly, repeatMonthly} {
		if got := presetOf(presetRule(choice, tuesday), tuesday); got != choice {
			t.Errorf("%s comes back as %s", choice, got)
		}
	}
	if got := presetOf(RepeatRule{Freq: repeatWeekly, Interval: 2, Days: []string{"tuesday"}}, tuesday); got != repeatCustom {
		t.Errorf("every two weeks is %s", got)
	}
}

func TestRecurrenceForTalk(t *testing.T) {
	start := tuesday.UTC()
	rule := RepeatRule{Freq: repeatWeekly, Interval: 2, Days: []string{"thursday", "tuesday"}, Until: "2026-12-10"}
	rec := rule.recurrence(start, ekb)
	if rec.Type != "weekly" || *rec.Interval != 2 || !slices.Equal(rec.DaysOfTheWeek, []string{"tuesday", "thursday"}) || rec.NumberOfEvents != nil {
		t.Fatalf("%+v", rec)
	}
	// The last day is included: its end in Yekaterinburg.
	if want := time.Date(2026, 12, 10, 23, 59, 59, 0, ekb); !rec.EndDate.Equal(want) {
		t.Errorf("end %v, want %v", rec.EndDate, want)
	}
	rec = RepeatRule{Freq: repeatMonthly, Interval: 1, Count: 4}.recurrence(start, ekb)
	if *rec.DayOfMonth != 10 || *rec.NumberOfEvents != 4 || rec.EndDate != nil {
		t.Fatalf("%+v", rec)
	}
}

func TestRepeatFormShowsWhatTheChoiceNeeds(t *testing.T) {
	names := func(elements []model.DialogElement) []string {
		out := []string{}
		for _, element := range elements {
			out = append(out, element.Name)
		}
		return out
	}
	start := tuesday
	if got := names(repeatElements(map[string]string{"repeat": repeatNone}, start)); !slices.Equal(got, []string{"repeat"}) {
		t.Errorf("none: %v", got)
	}
	weekly := repeatElements(map[string]string{"repeat": repeatWeekly}, start)
	if got := names(weekly); !slices.Equal(got, []string{"repeat", "repeat_end", "repeat_until"}) {
		t.Errorf("weekly: %v", got)
	}
	if weekly[2].Default != "2026-12-10" {
		t.Errorf("default end %s, want a month later", weekly[2].Default)
	}
	if !strings.Contains(weekly[0].HelpText, "Каждую неделю по вторникам до 10.12.2026") {
		t.Errorf("summary %q", weekly[0].HelpText)
	}
	labels := []string{}
	for _, option := range weekly[0].Options {
		labels = append(labels, option.Text)
	}
	if !slices.Contains(labels, "Еженедельно по вторникам") || !slices.Contains(labels, "Ежемесячно, 10-го числа") || !slices.Contains(labels, "Другое…") {
		t.Errorf("options %v", labels)
	}
	custom := names(repeatElements(map[string]string{"repeat": repeatCustom, "repeat_end": repeatEndCount}, start))
	if !slices.Equal(custom, []string{"repeat", "repeat_freq", "repeat_interval", "repeat_days", "repeat_end", "repeat_count"}) {
		t.Errorf("custom weekly: %v", custom)
	}
	monthly := names(repeatElements(map[string]string{"repeat": repeatCustom, "repeat_freq": repeatMonthly}, start))
	if slices.Contains(monthly, "repeat_days") {
		t.Errorf("monthly asks for days: %v", monthly)
	}
}

func TestRepeatInputIsChecked(t *testing.T) {
	cases := map[string]map[string]any{
		"repeat_interval": {"repeat": repeatCustom, "repeat_freq": repeatDaily, "repeat_interval": "0"},
		"repeat_days":     {"repeat": repeatCustom, "repeat_freq": repeatWeekly, "repeat_days": []any{}},
		"repeat_until":    {"repeat": repeatDaily, "repeat_until": "2026-11-10"},
		"repeat_count":    {"repeat": repeatDaily, "repeat_end": repeatEndCount, "repeat_count": "101"},
		"repeat":          {"repeat": "yearly"},
	}
	for field, sub := range cases {
		if _, errs := parseRepeat(sub, tuesday); errs[field] == "" {
			t.Errorf("%s: no error in %v", field, errs)
		}
	}
	rule, errs := parseRepeat(map[string]any{"repeat": repeatCustom, "repeat_freq": repeatWeekly, "repeat_interval": float64(2),
		"repeat_days": []any{"friday", "monday"}, "repeat_until": "2027-02-01"}, tuesday)
	if len(errs) != 0 || rule.Interval != 2 || !slices.Equal(rule.Days, []string{"monday", "friday"}) || rule.Until != "2027-02-01" {
		t.Fatalf("%+v %v", rule, errs)
	}
	if rule, _ := parseRepeat(map[string]any{"repeat": repeatNone}, tuesday); rule != nil {
		t.Error("no repeat must give no rule")
	}
}

func TestRefreshKeepsWhatWasTyped(t *testing.T) {
	req := &model.SubmitDialogRequest{Type: "refresh", State: "edit:m1", Submission: map[string]any{
		"title": "Планёрка", "start": "2026-11-10T07:00:00Z", "duration": "30", "repeat": repeatCustom, "repeat_freq": repeatWeekly,
		"repeat_days": []any{"monday", "thursday"}, "participants": []any{"Анна (@anna)", "@boris"}, "lobby": true, "selected_field": "repeat",
	}}
	dialog := refreshedScheduleDialog(req, ekb)
	byName := map[string]model.DialogElement{}
	for _, element := range dialog.Elements {
		byName[element.Name] = element
	}
	if dialog.Title != "Изменение" || dialog.State != "edit:m1" || dialog.SourceURL == "" {
		t.Errorf("dialog %q %q %q", dialog.Title, dialog.State, dialog.SourceURL)
	}
	if byName["title"].Default != "Планёрка" || byName["participants"].Default != "Анна (@anna),@boris" || byName["lobby"].Default != "true" || byName["repeat_days"].Default != "monday,thursday" {
		t.Errorf("values lost: %+v", byName)
	}
	if !strings.Contains(byName["repeat"].HelpText, "по понедельникам и четвергам") {
		t.Errorf("summary %q", byName["repeat"].HelpText)
	}
}

// Only the organizer in Talk gets the edit form back on refresh.
func TestRefreshOfAnEditChecksTheRight(t *testing.T) {
	p := rightsWorld(t)
	givePersonalKeys(t, p, "owner", "guest", "sysadmin", "chanadmin")
	ask := func(userID string) string {
		body := `{"type":"refresh","callback_id":"schedule","state":"edit:m1","submission":{"repeat":"none"}}`
		req := httptest.NewRequest(http.MethodPost, "/dialogs/schedule", strings.NewReader(body))
		req.Header.Set("Mattermost-User-Id", userID)
		rec := httptest.NewRecorder()
		p.authenticated(p.dialogSchedule).ServeHTTP(rec, withUser(req, userID))
		return rec.Body.String()
	}
	for _, user := range []string{"guest", "sysadmin", "chanadmin"} {
		if got := ask(user); !strings.Contains(got, "Недостаточно прав") || strings.Contains(got, `"form"`) {
			t.Errorf("%s got %s", user, got)
		}
	}
	if got := ask("owner"); !strings.Contains(got, `"type":"form"`) {
		t.Errorf("owner got %s", got)
	}
}

// withUser puts the session user into the body, as Mattermost does.
func withUser(req *http.Request, userID string) *http.Request {
	data, _ := io.ReadAll(req.Body)
	var body map[string]any
	_ = json.Unmarshal(data, &body)
	body["user_id"] = userID
	data, _ = json.Marshal(body)
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.ContentLength = int64(len(data))
	return req
}
