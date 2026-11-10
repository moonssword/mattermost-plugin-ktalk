package talk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL+"/", "secret-key")
}

func TestAuthHeaderAndPath(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Auth-Token"); got != "secret-key" {
			t.Errorf("token = %q", got)
		}
		if r.URL.Path != "/api/Rooms/abc" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"roomName":"abc","usersOnline":2,"onlineUsers":[{"isAnonymous":true,"anonymousName":"Гость"}],"sipSettings":{"sipNumber":"123"}}`))
	})
	room, err := c.GetRoom(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if room.UsersOnline != 2 || room.OnlineUsers[0].Name() != "Гость" || room.SIPSettings.SIPNumber != "123" {
		t.Fatalf("room = %+v", room)
	}
}

func TestErrorMapping(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorMessage":"no scope"}`))
	})
	_, err := c.GetRoom(context.Background(), "abc")
	if !IsStatus(err, http.StatusForbidden) {
		t.Fatalf("err = %v", err)
	}
	if IsNotFound(err) {
		t.Fatal("403 reported as not found")
	}
}

func TestPutRoomSendsNullGuestDate(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		value, present := body["anonymousAccessExpirationDate"]
		if !present || value != nil {
			t.Errorf("guest date must be an explicit null, got %v (present=%v)", value, present)
		}
		if body["enableLobby"] != true {
			t.Errorf("enableLobby = %v", body["enableLobby"])
		}
		_, _ = w.Write([]byte(`{"roomName":"abc"}`))
	})
	if _, err := c.PutRoom(context.Background(), "abc", RoomParams{EnableLobby: true}); err != nil {
		t.Fatal(err)
	}
}

func TestListEventsCapsPeriod(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("end"); got != "2026-11-08T00:00:00Z" {
			t.Errorf("end = %q", got)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"1","subject":"x"}]}`))
	})
	items, err := c.ListEvents(context.Background(), "a@b.c", start, start.Add(30*24*time.Hour))
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

func TestRepeatedEmailQuery(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query()["email"]; len(got) != 2 {
			t.Errorf("email = %v", got)
		}
		_, _ = w.Write([]byte(`{"users":[{"key":"k","firstname":"Анна","surname":"Иванова"}]}`))
	})
	users, err := c.FindUsersByEmail(context.Background(), []string{"a@b.c", "d@e.f"}, false)
	if err != nil || users[0].DisplayName() != "Анна Иванова" {
		t.Fatalf("users=%v err=%v", users, err)
	}
}

func TestActiveRecordingNotFoundIsEmpty(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	key, err := c.GetActiveRecording(context.Background(), "abc")
	if err != nil || key != "" {
		t.Fatalf("key=%q err=%v", key, err)
	}
}

func TestSummaryText(t *testing.T) {
	s := &Summary{Chunks: []SummaryChunk{{Text: " a "}, {Text: ""}, {Text: "b"}}}
	if got := s.Text(); got != "a\n\nb" {
		t.Fatalf("text = %q", got)
	}
	s.Hidden = true
	if s.Text() != "" {
		t.Fatal("hidden summary returned text")
	}
}

func TestAccessInfoScopes(t *testing.T) {
	a := &AccessInfo{Scopes: []Scope{{Type: "rooms", RestrictionType: "readWrite"}, {Type: "reporting", RestrictionType: "read"}}}
	if !a.CanWrite("rooms") || a.CanWrite("reporting") || !a.CanRead("reporting") || a.CanRead("webhooks") {
		t.Fatal("scope checks wrong")
	}
}

func TestPersonalTokenUsesOwnCalendar(t *testing.T) {
	var seen []string
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("X-Auth-Token") != "pat" {
			t.Errorf("token = %q", r.Header.Get("X-Auth-Token"))
		}
		_, _ = w.Write([]byte(`{"id":"e1","items":[]}`))
	}).WithPersonalToken("pat")
	ctx := context.Background()
	start := time.Date(2030, 1, 1, 10, 0, 0, 0, time.UTC)
	if !c.Personal() {
		t.Fatal("not personal")
	}
	_, _ = c.ListEvents(ctx, "ignored@example.com", start, start.Add(time.Hour))
	_, _ = c.CreateEvent(ctx, "ignored@example.com", CalendarEvent{Subject: "x", RoomName: "r"})
	_, _ = c.UpdateEvent(ctx, "ignored@example.com", "e1", CalendarEvent{Subject: "y", RoomName: "r"})
	_, _ = c.SetAttendees(ctx, "ignored@example.com", "e1", []string{"k"}, nil)
	_ = c.CancelEvent(ctx, "ignored@example.com", "e1", "причина")
	_ = c.RespondToEvent(ctx, "e1", true)
	_ = c.RespondToEvent(ctx, "e1", false)
	want := []string{
		"GET /api/Calendar?end=2030-01-01T11%3A00%3A00Z&start=2030-01-01T10%3A00%3A00Z",
		"POST /api/Calendar?",
		"PUT /api/Calendar/e1?",
		"POST /api/Calendar/e1/attendees?",
		"POST /api/Calendar/e1/cancel?message=%D0%BF%D1%80%D0%B8%D1%87%D0%B8%D0%BD%D0%B0",
		"POST /api/Calendar/e1/accept?",
		"POST /api/Calendar/e1/reject?withNotification=true",
	}
	if len(seen) != len(want) {
		t.Fatalf("calls %v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("call %d: got %q, want %q", i, seen[i], want[i])
		}
	}
}

func TestSpaceKeyKeepsEmailCalendar(t *testing.T) {
	var path string
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	})
	_ = c.CancelEvent(context.Background(), "a@b.c", "e1", "")
	if path != "DELETE /api/EmailCalendar/a@b.c/e1" {
		t.Fatalf("path %q", path)
	}
	if err := c.RespondToEvent(context.Background(), "e1", true); !errors.Is(err, ErrPersonalOnly) {
		t.Fatalf("space key answered an invitation: %v", err)
	}
	if c.WithPersonalToken("x").WithToken("y").Personal() {
		t.Fatal("WithToken kept the personal mode")
	}
}

func TestListEventsDropsDeletedEvents(t *testing.T) {
	// The shape the personal calendar returns: a live event and a deleted one.
	c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":"a","subject":"Живая","busyType":"busy","isCancelled":false},{"id":"b","subject":"Удалённая","busyType":0,"isCancelled":false},{"id":"c","subject":"Без типа"}]}`))
	}).WithPersonalToken("pat")
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	items, err := c.ListEvents(context.Background(), "", start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "a" || items[1].ID != "c" {
		t.Fatalf("items %+v", items)
	}
}

func TestGetSeriesReadsTheWholeSeries(t *testing.T) {
	var seen []string
	handler := func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		_, _ = w.Write([]byte(`{"id":"master","requiredAttendees":[{"name":"Анна","status":"accepted","user":{"key":"k1"}}]}`))
	}
	ctx := context.Background()
	series, err := newServer(t, handler).GetSeries(ctx, "org@example.com", "occurrence")
	if err != nil || series.ID != "master" {
		t.Fatalf("series %+v, err %v", series, err)
	}
	if _, err := newServer(t, handler).WithPersonalToken("pat").GetSeries(ctx, "", "occurrence"); err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/EmailCalendar/org@example.com/occurrence/recurrence", "/api/Calendar/occurrence/recurrence"}
	if len(seen) != 2 || seen[0] != want[0] || seen[1] != want[1] {
		t.Fatalf("paths %v, want %v", seen, want)
	}
}

func TestSetAttendeesSendsEmptyListsAsEmpty(t *testing.T) {
	var body string
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		_, _ = w.Write([]byte(`{"id":"e1"}`))
	})
	if _, err := c.SetAttendees(context.Background(), "org@example.com", "e1", []string{"k1"}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"requiredExternalAttendeesEmails":[]`) {
		t.Fatalf("body %s", body)
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("redirect followed with token %q", r.Header.Get("X-Auth-Token"))
	}))
	defer other.Close()
	client := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/api/Users/profile", http.StatusFound)
	}))
	_, err := client.GetProfile(context.Background())
	if !IsStatus(err, http.StatusFound) {
		t.Fatalf("want the redirect as an error, got %v", err)
	}
}

func TestSummaryPartsThatCarryJSON(t *testing.T) {
	hidden := SummaryChunk{Type: "protocolSum", Text: `{"title":"Скрыто"}`}
	hidden.HiddenStatus = &struct {
		IsHidden bool `json:"isHidden"`
	}{IsHidden: true}
	s := &Summary{Chunks: []SummaryChunk{
		{Type: "briefsummaryWithHighlights", Text: `{"headline":"Запуск","summary":"Договорились о сроках."}`},
		{Type: "protocolThemes", Text: `{"title":"Темы","topics":[{"title":"Бюджет","content":"Утвердили."}]}`},
		{Type: "protocolNextSteps", Text: `{"title":"Задачи","tasks":[{"task":"Отчёт","responsible":"Анна","deadline":"пятница"},{"task":"Счёт"}]}`},
		hidden,
		{Type: "briefsummary", Text: "{не JSON"},
		{Type: "briefsummary", Text: "Обычный текст"},
	}}
	want := "**Запуск**\n\nДоговорились о сроках." +
		"\n\n**Темы**\n\n- **Бюджет** · Утвердили." +
		"\n\n**Задачи**\n\n- Отчёт\n  - Ответственный: Анна\n  - Срок: пятница\n- Счёт" +
		"\n\n{не JSON\n\nОбычный текст"
	if got := s.Text(); got != want {
		t.Fatalf("text =\n%s\nwant\n%s", got, want)
	}
}
