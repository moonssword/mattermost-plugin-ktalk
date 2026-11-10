package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/moonssword/mattermost-plugin-ktalk/server/talk"
)

// The repeat choices of the scheduling form, worded as in Talk's own form.
const (
	repeatNone     = "none"
	repeatWeekdays = "weekdays"
	repeatDaily    = "daily"
	repeatWeekly   = "weekly"
	repeatMonthly  = "monthly"
	repeatCustom   = "custom"

	repeatEndDate  = "until"
	repeatEndCount = "count"

	maxRepeatInterval = 99
)

// weekdays in Talk's spelling, Monday first as in Russian calendars.
var weekdays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

var workweek = weekdays[:5]

var weekdayShort = map[string]string{
	"monday": "Пн", "tuesday": "Вт", "wednesday": "Ср", "thursday": "Чт", "friday": "Пт", "saturday": "Сб", "sunday": "Вс",
}

// weekdayPlural is "по понедельникам" and so on.
var weekdayPlural = map[string]string{
	"monday": "понедельникам", "tuesday": "вторникам", "wednesday": "средам", "thursday": "четвергам",
	"friday": "пятницам", "saturday": "субботам", "sunday": "воскресеньям",
}

// RepeatRule is how a scheduled meeting repeats. It mirrors what Talk's form
// offers: a period with an interval, the days of the week for a weekly
// meeting, and an end either on a date or after a number of meetings.
type RepeatRule struct {
	Freq       string   `json:"freq"`
	Interval   int      `json:"interval"`
	Days       []string `json:"days,omitempty"`
	DayOfMonth int      `json:"day_of_month,omitempty"`
	// Until is the last day as YYYY-MM-DD in the organizer's time zone.
	Until string `json:"until,omitempty"`
	Count int    `json:"count,omitempty"`
}

func weekdayName(t time.Time) string {
	return strings.ToLower(t.Weekday().String())
}

// presetRule is the rule behind a short choice for a meeting starting at
// start, in the organizer's zone.
func presetRule(choice string, start time.Time) RepeatRule {
	switch choice {
	case repeatWeekdays:
		return RepeatRule{Freq: repeatWeekly, Interval: 1, Days: slices.Clone(workweek)}
	case repeatDaily:
		return RepeatRule{Freq: repeatDaily, Interval: 1}
	case repeatWeekly:
		return RepeatRule{Freq: repeatWeekly, Interval: 1, Days: []string{weekdayName(start)}}
	case repeatMonthly:
		return RepeatRule{Freq: repeatMonthly, Interval: 1, DayOfMonth: start.Day()}
	}
	return RepeatRule{}
}

// presetOf names the short choice a rule matches, or custom.
func presetOf(rule RepeatRule, start time.Time) string {
	for _, choice := range []string{repeatWeekdays, repeatDaily, repeatWeekly, repeatMonthly} {
		preset := presetRule(choice, start)
		if preset.Freq == rule.Freq && preset.Interval == rule.Interval && slices.Equal(preset.Days, rule.Days) &&
			(rule.Freq != repeatMonthly || preset.DayOfMonth == rule.DayOfMonth) {
			return choice
		}
	}
	return repeatCustom
}

// sortDays orders days from Monday and drops unknown and repeated ones.
func sortDays(days []string) []string {
	out := []string{}
	for _, day := range weekdays {
		if slices.Contains(days, day) {
			out = append(out, day)
		}
	}
	return out
}

// plural picks the Russian form for a count: one, few, many.
func plural(n int, one, few, many string) string {
	tens, ones := n%100, n%10
	switch {
	case tens >= 11 && tens <= 14:
		return many
	case ones == 1:
		return one
	case ones >= 2 && ones <= 4:
		return few
	}
	return many
}

// daysText is "по будням" or "по вторникам и четвергам".
func daysText(days []string) string {
	if slices.Equal(days, workweek) {
		return "по будням"
	}
	names := make([]string, 0, len(days))
	for _, day := range days {
		names = append(names, weekdayPlural[day])
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return "по " + names[0]
	}
	return "по " + strings.Join(names[:len(names)-1], ", ") + " и " + names[len(names)-1]
}

// periodText is "каждую неделю", "каждые 2 недели" and so on.
func periodText(freq string, interval int) string {
	if interval <= 1 {
		return map[string]string{repeatDaily: "Каждый день", repeatWeekly: "Каждую неделю", repeatMonthly: "Каждый месяц"}[freq]
	}
	unit := map[string][3]string{
		repeatDaily:   {"день", "дня", "дней"},
		repeatWeekly:  {"неделю", "недели", "недель"},
		repeatMonthly: {"месяц", "месяца", "месяцев"},
	}[freq]
	every := "Каждые"
	if interval%10 == 1 && interval%100 != 11 {
		every = map[string]string{repeatDaily: "Каждый", repeatWeekly: "Каждую", repeatMonthly: "Каждый"}[freq]
	}
	return fmt.Sprintf("%s %d %s", every, interval, plural(interval, unit[0], unit[1], unit[2]))
}

// Text describes the rule the way Talk's form does, for example "Каждую
// неделю по вторникам до 10.12.2026".
func (r RepeatRule) Text() string {
	text := periodText(r.Freq, r.Interval)
	switch r.Freq {
	case repeatWeekly:
		if days := daysText(r.Days); days != "" {
			text += " " + days
		}
	case repeatMonthly:
		if r.DayOfMonth > 0 {
			text += fmt.Sprintf(", %d-го числа", r.DayOfMonth)
		}
	}
	switch {
	case r.Until != "":
		if day, err := time.Parse(time.DateOnly, r.Until); err == nil {
			text += " до " + day.Format("02.01.2006")
		}
	case r.Count > 0:
		text += fmt.Sprintf(", %d %s", r.Count, plural(r.Count, "встреча", "встречи", "встреч"))
	}
	return text
}

// recurrence is the rule in the form the Talk calendar takes. The last day
// ends at its midnight in the organizer's zone, so it is included.
func (r RepeatRule) recurrence(start time.Time, loc *time.Location) *talk.Recurrence {
	interval := max(r.Interval, 1)
	rec := &talk.Recurrence{Type: r.Freq, StartDate: &start, Interval: &interval}
	switch r.Freq {
	case repeatWeekly:
		rec.DaysOfTheWeek = sortDays(r.Days)
	case repeatMonthly:
		day := r.DayOfMonth
		if day == 0 {
			day = start.In(loc).Day()
		}
		rec.DayOfMonth = &day
	}
	if r.Until != "" {
		if day, err := time.ParseInLocation(time.DateOnly, r.Until, loc); err == nil {
			end := day.AddDate(0, 0, 1).Add(-time.Second).UTC()
			rec.EndDate = &end
		}
	} else if r.Count > 0 {
		count := r.Count
		rec.NumberOfEvents = &count
	}
	return rec
}

// legacyRule reads a meeting stored before rules existed: a period and a count.
func legacyRule(repeat string, count int, start time.Time) RepeatRule {
	rule := presetRule(repeat, start)
	rule.Count = count
	return rule
}

// --- The repeat part of the form ---

// repeatValues fills the repeat fields of the form from a rule.
func repeatValues(values map[string]string, rule *RepeatRule, start time.Time) {
	if rule == nil || rule.Freq == "" {
		values["repeat"] = repeatNone
		return
	}
	values["repeat"] = presetOf(*rule, start)
	values["repeat_freq"] = rule.Freq
	values["repeat_interval"] = strconv.Itoa(max(rule.Interval, 1))
	values["repeat_days"] = strings.Join(rule.Days, ",")
	if rule.Until != "" {
		values["repeat_end"] = repeatEndDate
		values["repeat_until"] = rule.Until
	} else {
		values["repeat_end"] = repeatEndCount
		values["repeat_count"] = strconv.Itoa(rule.Count)
	}
}

// defaultUntil is a month after the start, as Talk suggests.
func defaultUntil(start time.Time) string {
	return start.AddDate(0, 1, 0).Format(time.DateOnly)
}

// formStart reads the start for labels that depend on it, falling back to
// the next full hour while the form has no valid date yet.
func formStart(values map[string]string, loc *time.Location, now time.Time) time.Time {
	if start, err := time.Parse(time.RFC3339, values["start"]); err == nil {
		return start.In(loc)
	}
	return now.In(loc).Truncate(time.Hour).Add(time.Hour)
}

// repeatElements builds the repeat fields. The short choices are named after
// the start date; "Другое…" opens the full set, as Talk's form does.
func repeatElements(values map[string]string, start time.Time) []model.DialogElement {
	option := func(text, value string) *model.PostActionOptions {
		return &model.PostActionOptions{Text: text, Value: value}
	}
	choice := values["repeat"]
	if choice == "" {
		choice = repeatNone
	}
	elements := []model.DialogElement{{
		DisplayName: "Повтор", Name: "repeat", Type: "select", Default: choice, Refresh: true,
		Options: []*model.PostActionOptions{
			option("Не повторять", repeatNone),
			option("По будням", repeatWeekdays),
			option("Каждый день", repeatDaily),
			option("Еженедельно "+daysText([]string{weekdayName(start)}), repeatWeekly),
			option(fmt.Sprintf("Ежемесячно, %d-го числа", start.Day()), repeatMonthly),
			option("Другое…", repeatCustom),
		},
	}}
	if choice == repeatNone {
		return elements
	}

	if choice == repeatCustom {
		freq := values["repeat_freq"]
		if freq == "" {
			freq = repeatWeekly
		}
		interval := values["repeat_interval"]
		if interval == "" {
			interval = "1"
		}
		unit := map[string]string{repeatDaily: "дн.", repeatWeekly: "нед.", repeatMonthly: "мес."}[freq]
		elements = append(elements,
			model.DialogElement{DisplayName: "Как часто", Name: "repeat_freq", Type: "select", Default: freq, Refresh: true,
				Options: []*model.PostActionOptions{option("По дням", repeatDaily), option("По неделям", repeatWeekly), option("По месяцам", repeatMonthly)}},
			model.DialogElement{DisplayName: "Каждые", Name: "repeat_interval", Type: "text", SubType: "number", Default: interval,
				HelpText: fmt.Sprintf("Интервал в %s, от 1 до %d.", unit, maxRepeatInterval)},
		)
		if freq == repeatWeekly {
			days := values["repeat_days"]
			if days == "" {
				days = weekdayName(start)
			}
			dayOptions := make([]*model.PostActionOptions, 0, len(weekdays))
			for _, day := range weekdays {
				dayOptions = append(dayOptions, option(weekdayShort[day], day))
			}
			elements = append(elements, model.DialogElement{DisplayName: "Дни недели", Name: "repeat_days", Type: "select",
				MultiSelect: true, Default: days, Options: dayOptions, Refresh: true})
		}
	}

	end := values["repeat_end"]
	if end == "" {
		end = repeatEndDate
	}
	elements = append(elements, model.DialogElement{DisplayName: "Окончание", Name: "repeat_end", Type: "radio", Default: end, Refresh: true,
		Options: []*model.PostActionOptions{option("В дату", repeatEndDate), option("После числа встреч", repeatEndCount)}})
	if end == repeatEndCount {
		count := values["repeat_count"]
		if count == "" {
			count = "10"
		}
		elements = append(elements, model.DialogElement{DisplayName: "Число встреч", Name: "repeat_count", Type: "text", SubType: "number",
			Default: count, HelpText: fmt.Sprintf("От 2 до %d.", maxRepeatCount)})
	} else {
		until := values["repeat_until"]
		if until == "" {
			until = defaultUntil(start)
		}
		elements = append(elements, model.DialogElement{DisplayName: "До даты", Name: "repeat_until", Type: "date",
			Default: until, Refresh: true, DateTimeConfig: &model.DialogDateTimeConfig{MinDate: "today"}})
	}

	// "Что получилось" follows the choices, as in Talk.
	if rule, errs := parseRepeat(stringMap(values), start); len(errs) == 0 && rule != nil {
		elements[0].HelpText = "Что получилось: " + rule.Text() + "."
	}
	return elements
}

// stringMap lets the form's string values go through the submission parser.
func stringMap(values map[string]string) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

// parseRepeat reads the repeat fields. A nil rule means a single meeting.
func parseRepeat(sub map[string]any, start time.Time) (*RepeatRule, map[string]string) {
	errs := map[string]string{}
	choice := stringField(sub, "repeat")
	var rule RepeatRule
	switch choice {
	case "", repeatNone:
		return nil, errs
	case repeatWeekdays, repeatDaily, repeatWeekly, repeatMonthly:
		rule = presetRule(choice, start)
	case repeatCustom:
		rule.Freq = stringField(sub, "repeat_freq")
		if rule.Freq == "" {
			rule.Freq = repeatWeekly
		}
		if rule.Freq != repeatDaily && rule.Freq != repeatWeekly && rule.Freq != repeatMonthly {
			errs["repeat_freq"] = "Неизвестный вариант."
		}
		interval, ok := 1, true
		if raw, given := sub["repeat_interval"]; given && raw != "" {
			interval, ok = intField(sub, "repeat_interval")
		}
		if !ok || interval < 1 || interval > maxRepeatInterval {
			errs["repeat_interval"] = fmt.Sprintf("Укажите число от 1 до %d.", maxRepeatInterval)
		}
		rule.Interval = interval
		switch rule.Freq {
		case repeatWeekly:
			rule.Days = sortDays(listField(sub, "repeat_days"))
			if _, given := sub["repeat_days"]; !given {
				rule.Days = []string{weekdayName(start)}
			}
			if len(rule.Days) == 0 {
				errs["repeat_days"] = "Выберите хотя бы один день."
			}
		case repeatMonthly:
			rule.DayOfMonth = start.Day()
		}
	default:
		errs["repeat"] = "Неизвестный вариант."
		return nil, errs
	}

	if stringField(sub, "repeat_end") == repeatEndCount {
		count, ok := intField(sub, "repeat_count")
		if !ok || count < 2 || count > maxRepeatCount {
			errs["repeat_count"] = fmt.Sprintf("Укажите число встреч от 2 до %d.", maxRepeatCount)
		}
		rule.Count = count
	} else {
		until := stringField(sub, "repeat_until")
		if until == "" {
			until = defaultUntil(start)
		}
		day, err := time.Parse(time.DateOnly, until)
		switch {
		case err != nil:
			errs["repeat_until"] = "Укажите дату."
		case day.Format(time.DateOnly) <= start.Format(time.DateOnly):
			errs["repeat_until"] = "Дата должна быть позже первой встречи."
		default:
			rule.Until = day.Format(time.DateOnly)
		}
	}
	return &rule, errs
}
