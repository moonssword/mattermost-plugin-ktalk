package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

const commandTrigger = "talk"

const helpText = `**Контур.Толк**
* ` + "`/talk start [тема]`" + ` начать встречу. В личных и групповых чатах участникам придёт звонок в Толке.
* ` + "`/talk link [тема]`" + ` отправить ссылку на встречу без звонка.
* ` + "`/talk room`" + ` ваша постоянная комната.
* ` + "`/talk schedule`" + ` запланировать встречу.
* ` + "`/talk pin <код|off>`" + ` задать или снять ПИН-код.
* ` + "`/talk guests <часы|off>`" + ` открыть или закрыть вход для внешних участников.
* ` + "`/talk lobby <on|off>`" + ` включить или выключить зал ожидания.
* ` + "`/talk record`" + ` начать запись.
* ` + "`/talk end`" + ` завершить встречу для всех.
* ` + "`/talk connect`" + ` подключить личный ключ Толка, ` + "`/talk disconnect`" + ` отключить его.
* ` + "`/talk settings`" + ` личные настройки.
* ` + "`/talk status`" + ` состояние подключения.

Команды ` + "`pin`, `guests`, `lobby`, `record`, `end`" + ` действуют на последнюю активную встречу канала.`

func talkCommand() *model.Command {
	data := model.NewAutocompleteData(commandTrigger, "[команда]", "Встречи в Контур.Толке")
	add := func(trigger, hint, help string) *model.AutocompleteData {
		sub := model.NewAutocompleteData(trigger, hint, help)
		data.AddCommand(sub)
		return sub
	}
	add("start", "[тема]", "Начать встречу")
	add("link", "[тема]", "Отправить ссылку на встречу, без звонка")
	add("room", "", "Моя постоянная комната")
	add("schedule", "", "Запланировать встречу")
	add("pin", "<код|off>", "Задать или снять ПИН-код")
	add("guests", "<часы|off>", "Вход для внешних участников")
	lobby := add("lobby", "", "Зал ожидания")
	lobby.AddStaticListArgument("Состояние", true, []model.AutocompleteListItem{{Item: "on", HelpText: "Включить"}, {Item: "off", HelpText: "Выключить"}})
	add("record", "", "Начать запись")
	add("end", "", "Завершить встречу для всех")
	add("connect", "", "Подключить личный ключ Толка")
	add("disconnect", "", "Отключить личный ключ Толка")
	add("settings", "", "Личные настройки")
	add("status", "", "Состояние подключения")
	add("help", "", "Справка")
	return &model.Command{
		Trigger:          commandTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "Встречи в Контур.Толке",
		AutoCompleteHint: "[start|room|schedule|pin|guests|lobby|record|end|connect|settings|status|help]",
		DisplayName:      "Контур.Толк",
		AutocompleteData: data,
	}
}

func ephemeral(text string) *model.CommandResponse {
	return &model.CommandResponse{ResponseType: model.CommandResponseTypeEphemeral, Text: text}
}

// splitCommand returns the subcommand and the rest of the line.
func splitCommand(line string) (sub, rest string) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", ""
	}
	sub = strings.ToLower(fields[1])
	if index := strings.Index(line, fields[1]); index >= 0 {
		rest = strings.TrimSpace(line[index+len(fields[1]):])
	}
	return sub, rest
}

// ExecuteCommand handles /talk. Errors reach the user as ephemeral text, so
// the AppError result the hook requires is always nil.
//
//nolint:unparam // Signature of plugin.Hooks.
func (p *Plugin) ExecuteCommand(_ *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	if !p.userAllowed(args.UserId) {
		return ephemeral(testModeMessage), nil
	}
	sub, rest := splitCommand(args.Command)
	response, err := p.runCommand(sub, rest, args)
	if err != nil {
		var known *userError
		if errors.As(err, &known) {
			return ephemeral(known.message), nil
		}
		p.API.LogError("Command failed", "command", sub, "error", err.Error())
		return ephemeral("Не удалось выполнить команду. Подробности в журнале сервера."), nil
	}
	return response, nil
}

func (p *Plugin) runCommand(sub, rest string, args *model.CommandArgs) (*model.CommandResponse, error) {
	switch sub {
	case "", "start":
		return p.commandStart(rest, args, false)
	case "link":
		return p.commandStart(rest, args, true)
	case "room":
		return p.commandRoom(args)
	case "schedule":
		return p.commandSchedule(rest, args)
	case "pin", "guests", "lobby", "record", "end":
		return p.commandControl(sub, rest, args)
	case "connect":
		return p.commandConnect(args)
	case "disconnect":
		if err := p.disconnectPersonalKey(args.UserId); err != nil {
			return nil, err
		}
		return ephemeral("Личный ключ Толка отключён. Планирование, управление встречами и записи станут доступны снова, когда вы подключите ключ."), nil
	case "settings":
		return p.commandSettings(rest, args)
	case "status":
		return p.commandStatus(args)
	case "help":
		return ephemeral(helpText), nil
	default:
		return ephemeral("Неизвестная команда.\n\n" + helpText), nil
	}
}

func (p *Plugin) commandStart(title string, args *model.CommandArgs, linkOnly bool) (*model.CommandResponse, error) {
	if p.userPref(args.UserId, prefNaming, p.getConfig().NamingScheme) == schemeAsk {
		return p.openStartDialog(title, args, linkOnly)
	}
	m, err := p.startMeeting(args.UserId, createRequest{ChannelID: args.ChannelId, RootID: args.RootId, Title: title, LinkOnly: linkOnly})
	if err != nil {
		return nil, err
	}
	return &model.CommandResponse{GotoLocation: m.URL}, nil
}

func (p *Plugin) commandRoom(args *model.CommandArgs) (*model.CommandResponse, error) {
	user, appErr := p.API.GetUser(args.UserId)
	if appErr != nil {
		return nil, appErr
	}
	link, err := p.personalRoom(user)
	if err != nil {
		return nil, err
	}
	return ephemeral("Ваша постоянная комната: " + link), nil
}

func (p *Plugin) commandControl(sub, rest string, args *model.CommandArgs) (*model.CommandResponse, error) {
	m, err := p.latestChannelMeeting(args.ChannelId)
	if err != nil {
		return nil, err
	}
	value := strings.ToLower(strings.TrimSpace(rest))
	switch sub {
	case "pin":
		if value == "" {
			return ephemeral("Укажите код из 4–6 цифр или `off`."), nil
		}
		if value == "off" {
			value = ""
		}
		if err := p.setPin(args.UserId, m.ID, value); err != nil {
			return nil, err
		}
		if value == "" {
			return ephemeral("ПИН-код снят."), nil
		}
		return ephemeral("ПИН-код задан."), nil
	case "guests":
		hours := 0
		if value != "off" {
			hours, err = strconv.Atoi(value)
			if err != nil || hours <= 0 || hours > 24*30 {
				return ephemeral("Укажите число часов (не больше 720) или `off`."), nil
			}
		}
		if err := p.setGuests(args.UserId, m.ID, hours); err != nil {
			return nil, err
		}
		if hours == 0 {
			return ephemeral("Вход для внешних участников закрыт."), nil
		}
		return ephemeral(fmt.Sprintf("Внешние участники могут войти в течение %d ч.", hours)), nil
	case "lobby":
		if value != "on" && value != "off" {
			return ephemeral("Укажите `on` или `off`."), nil
		}
		if err := p.setLobby(args.UserId, m.ID, value == "on"); err != nil {
			return nil, err
		}
		if value == "on" {
			return ephemeral("Зал ожидания включён."), nil
		}
		return ephemeral("Зал ожидания выключен."), nil
	case "record":
		if err := p.startRecording(args.UserId, m.ID, ""); err != nil {
			return nil, err
		}
		return ephemeral("Запись начата."), nil
	case "end":
		if err := p.endMeeting(args.UserId, m.ID); err != nil {
			return nil, err
		}
		return ephemeral("Встреча завершена для всех."), nil
	}
	return ephemeral(helpText), nil
}

func (p *Plugin) commandSettings(rest string, args *model.CommandArgs) (*model.CommandResponse, error) {
	fields := strings.Fields(strings.ToLower(rest))
	if len(fields) == 2 {
		name, value := fields[0], fields[1]
		valid := map[string][]string{
			prefNaming:        {schemeWords, schemeUUID, schemeContext, schemeAsk},
			prefOpenMode:      {"tab", "window"},
			prefNotifyRec:     {"true", "false"},
			prefAcceptCalls:   {"true", "false"},
			prefStyle:         {"default", styleCurrent, styleCards, styleCompact},
			prefMeetingStatus: {"true", "false"},
		}
		options, known := valid[name]
		if !known || !contains(options, value) {
			return ephemeral("Неизвестная настройка или значение.\n\n" + p.settingsText(args.UserId)), nil
		}
		if err := p.setUserPref(args.UserId, name, value); err != nil {
			return nil, err
		}
		return ephemeral("Сохранено.\n\n" + p.settingsText(args.UserId)), nil
	}
	return ephemeral(p.settingsText(args.UserId)), nil
}

func (p *Plugin) settingsText(userID string) string {
	cfg := p.getConfig()
	return fmt.Sprintf("**Личные настройки** (также в «Настройки → Контур.Толк»):\n"+
		"* `naming` схема имён комнат: **%s** (words, uuid, context, ask)\n"+
		"* `open_mode` как открывать встречу: **%s** (tab, window)\n"+
		"* `notify_recording` писать в личные сообщения о готовности записи: **%s** (true, false)\n"+
		"* `accept_calls` принимать звонки из Mattermost: **%s** (true, false)\n"+
		"* `style` вид карточек и панели: **%s** (default, current, cards, compact)\n"+
		"* `meeting_status` показывать в статусе Mattermost, что вы на встрече: **%s** (true, false)\n\n"+
		"Изменить: `/talk settings <настройка> <значение>`",
		p.userPref(userID, prefNaming, cfg.NamingScheme), p.userPref(userID, prefOpenMode, "tab"),
		p.userPref(userID, prefNotifyRec, "false"), p.userPref(userID, prefAcceptCalls, "true"),
		p.userPref(userID, prefStyle, "default"), p.userPref(userID, prefMeetingStatus, "true"))
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func (p *Plugin) commandStatus(args *model.CommandArgs) (*model.CommandResponse, error) {
	cfg := p.getConfig()
	if cfg.spaceURL() == "" {
		return ephemeral("Плагин не настроен: не указан адрес пространства."), nil
	}
	lines := []string{"Пространство: " + cfg.spaceURL()}
	client := p.client()
	if client == nil {
		lines = append(lines, "Режим: только ссылки, API-ключ не задан.")
	} else {
		ctx, cancel := apiContext()
		defer cancel()
		info, err := client.GetAccessInfo(ctx)
		if err != nil {
			p.noteAPIError(err)
			lines = append(lines, "API-ключ: ошибка проверки.")
		} else {
			lines = append(lines, "API-ключ действует до "+info.ExpiredAt.Format("02.01.2006")+".")
		}
	}
	user, appErr := p.API.GetUser(args.UserId)
	if appErr == nil {
		if problem := p.emailProblem(user); problem != nil {
			lines = append(lines, problem.message)
		} else if identity, ok := p.talkUser(user); ok {
			lines = append(lines, "Вы в Толке: "+identity.Name+".")
		} else {
			lines = append(lines, notInTalkError().message)
		}
	}
	var stored personalKey
	if found, _ := p.kvGet(keyPAT+args.UserId, &stored); found {
		lines = append(lines, personalKeyConnected(stored))
	}
	return ephemeral(strings.Join(lines, "\n")), nil
}

// --- Dialogs ---

func (p *Plugin) dialogURL(name string) string {
	return "/plugins/" + pluginID + "/dialogs/" + name
}

// linkState marks a start dialog that must not ring, in front of the root id.
const linkState = "link:"

func (p *Plugin) openStartDialog(title string, args *model.CommandArgs, linkOnly bool) (*model.CommandResponse, error) {
	state := args.RootId
	if linkOnly {
		state = linkState + state
	}
	dialog := model.Dialog{
		CallbackId:  "start",
		Title:       "Встреча",
		SubmitLabel: "Начать",
		State:       state,
		Elements: []model.DialogElement{
			{DisplayName: "Тема", Name: "title", Type: "text", Default: title, Optional: true, MaxLength: maxTitleLength},
			{DisplayName: "Ключ", Name: "room_name", Type: "text", Optional: true, MaxLength: maxRoomNameLength,
				HelpText: "Строчные латинские буквы и цифры. Пусто: сгенерировать автоматически."},
		},
	}
	return p.openDialog(args.TriggerId, "start", dialog)
}

func (p *Plugin) openDialog(triggerID, name string, dialog model.Dialog) (*model.CommandResponse, error) {
	request := model.OpenDialogRequest{TriggerId: triggerID, URL: p.dialogURL(name), Dialog: dialog}
	if appErr := p.API.OpenInteractiveDialog(request); appErr != nil {
		return nil, appErr
	}
	return &model.CommandResponse{}, nil
}

func (p *Plugin) commandConnect(args *model.CommandArgs) (*model.CommandResponse, error) {
	if p.client() == nil {
		return ephemeral("Сначала администратор должен указать API-ключ пространства."), nil
	}
	dialog := model.Dialog{
		CallbackId:       "connect",
		Title:            "Личный ключ Толка",
		IntroductionText: "В Толке нажмите на аватар → **Настройки → API ключи → Выпустить ключ** и вставьте ключ сюда. С ним можно планировать встречи, управлять ими, открывать свою комнату и смотреть записи; в Толке всё это выполняется от вашего имени. Ключ хранится в зашифрованном виде.",
		SubmitLabel:      "Подключить",
		Elements:         []model.DialogElement{{DisplayName: "Ключ", Name: "token", Type: "text", SubType: "password", MaxLength: 2000}},
	}
	return p.openDialog(args.TriggerId, "connect", dialog)
}

func (p *Plugin) commandSchedule(rest string, args *model.CommandArgs) (*model.CommandResponse, error) {
	user, appErr := p.API.GetUser(args.UserId)
	if appErr != nil {
		return nil, appErr
	}
	if err := p.checkCanStart(user, args.ChannelId); err != nil {
		return nil, err
	}
	// Planning is done as the user, so the key is asked for before the form.
	if _, err := p.userClient(args.UserId); err != nil {
		return nil, err
	}
	// `/talk schedule edit <id>` opens the same dialog for an existing meeting.
	fields := strings.Fields(rest)
	if len(fields) == 2 && fields[0] == "edit" {
		m, err := p.controlledMeeting(args.UserId, fields[1], rightSchedule)
		if err != nil {
			return nil, err
		}
		if m.Schedule == nil {
			return ephemeral("Это не запланированная встреча."), nil
		}
		return p.openDialog(args.TriggerId, "schedule", scheduleDialog(m, p.participantValues(m.Schedule.UserIDs), args.RootId, userLocation(user).String()))
	}
	return p.openDialog(args.TriggerId, "schedule", scheduleDialog(nil, "", args.RootId, userLocation(user).String()))
}

// scheduleDialog builds the form. With a meeting it is filled in for editing,
// and the meeting id travels in the state field. Participants come as the
// readable values the form shows (see participantValue).
func scheduleDialog(m *Meeting, participants, rootID, timezone string) model.Dialog {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	values := map[string]string{"duration": "60", "repeat": repeatNone, "lobby": "false", "guests": "false", "auto_record": "false", "sip": "false"}
	state := "new:" + rootID
	if m != nil && m.Schedule != nil {
		sc := m.Schedule
		state = "edit:" + m.ID
		values["title"] = m.Title
		values["start"] = sc.Start.UTC().Format(time.RFC3339)
		values["duration"] = strconv.Itoa(int(sc.End.Sub(sc.Start).Minutes()))
		values["description"] = sc.Description
		rule := sc.Rule
		if rule == nil && sc.Recurring && sc.Repeat != "" {
			legacy := legacyRule(sc.Repeat, sc.RepeatCount, sc.Start.In(loc))
			rule = &legacy
		}
		repeatValues(values, rule, sc.Start.In(loc))
		values["participants"] = participants
		values["external"] = strings.Join(sc.ExternalEmails, ", ")
		values["pin"] = m.Pin
		values["lobby"] = strconv.FormatBool(m.Lobby)
		values["guests"] = strconv.FormatBool(sc.AllowGuests)
		values["auto_record"] = strconv.FormatBool(sc.AutoRecord)
		values["sip"] = strconv.FormatBool(sc.EnableSIP)
	}
	return scheduleForm(values, state, loc, time.Now())
}

// refreshedScheduleDialog rebuilds the form after a field with refresh
// changed, keeping everything typed so far.
func refreshedScheduleDialog(req *model.SubmitDialogRequest, loc *time.Location) *model.Dialog {
	values := map[string]string{}
	for key, value := range req.Submission {
		switch typed := value.(type) {
		case string:
			values[key] = typed
		case bool:
			values[key] = strconv.FormatBool(typed)
		case float64:
			values[key] = strconv.FormatFloat(typed, 'f', -1, 64)
		case []any:
			values[key] = strings.Join(listField(req.Submission, key), ",")
		}
	}
	// A new first date moves the default last date with it.
	if stringField(req.Submission, "selected_field") == "start" && values["repeat_end"] != repeatEndCount {
		values["repeat_until"] = ""
	}
	// A new kind of repeat starts from its own defaults.
	if stringField(req.Submission, "selected_field") == "repeat_freq" {
		values["repeat_days"] = ""
	}
	dialog := scheduleForm(values, req.State, loc, time.Now())
	return &dialog
}

// scheduleForm lays out the scheduling form from its values. The order
// follows Talk's own form: topic, time, repeat, people, then the room.
func scheduleForm(values map[string]string, state string, loc *time.Location, now time.Time) model.Dialog {
	options := func(pairs ...string) []*model.PostActionOptions {
		out := make([]*model.PostActionOptions, 0, len(pairs)/2)
		for i := 0; i+1 < len(pairs); i += 2 {
			out = append(out, &model.PostActionOptions{Text: pairs[i], Value: pairs[i+1]})
		}
		return out
	}
	title := "Планирование"
	if strings.HasPrefix(state, "edit:") {
		title = "Изменение"
	}
	elements := []model.DialogElement{
		{DisplayName: "Тема", Name: "title", Type: "text", Default: values["title"], MaxLength: maxTitleLength},
		{DisplayName: "Начало", Name: "start", Type: "datetime", Default: values["start"], Refresh: true,
			DateTimeConfig: &model.DialogDateTimeConfig{TimeInterval: 15, LocationTimezone: loc.String(), MinDate: "today"}},
		{DisplayName: "Длительность", Name: "duration", Type: "select", Default: values["duration"],
			Options: options("15 минут", "15", "30 минут", "30", "45 минут", "45", "1 час", "60", "1,5 часа", "90", "2 часа", "120", "3 часа", "180")},
	}
	elements = append(elements, repeatElements(values, formStart(values, loc, now))...)
	elements = append(elements,
		// Mattermost 11 drops the preselected users of a "users" select,
		// so an edit would silently remove everyone. A dynamic select keeps
		// them, and the plugin answers its searches.
		model.DialogElement{DisplayName: "Участники", Name: "participants", Type: "select", DataSource: "dynamic", DataSourceURL: "/plugins/" + pluginID + "/dialogs/users",
			MultiSelect: true, Optional: true, Default: values["participants"], Placeholder: "Начните вводить имя"},
		model.DialogElement{DisplayName: "Внешние", Name: "external", Type: "text", Optional: true, Default: values["external"], MaxLength: 1000,
			Placeholder: "Адреса почты через запятую"},
		model.DialogElement{DisplayName: "Описание", Name: "description", Type: "textarea", Optional: true, Default: values["description"], MaxLength: 2000},
		model.DialogElement{DisplayName: "ПИН-код", Name: "pin", Type: "text", Optional: true, Default: values["pin"], MaxLength: 6, Placeholder: "4–6 цифр"},
		model.DialogElement{DisplayName: "Зал ожидания", Name: "lobby", Type: "bool", Optional: true, Default: values["lobby"], Placeholder: "Впускать участников по запросу"},
		model.DialogElement{DisplayName: "Гости", Name: "guests", Type: "bool", Optional: true, Default: values["guests"], Placeholder: "Разрешить вход по ссылке без аккаунта"},
		model.DialogElement{DisplayName: "Автозапись", Name: "auto_record", Type: "bool", Optional: true, Default: values["auto_record"], Placeholder: "Записывать встречу автоматически"},
		model.DialogElement{DisplayName: "SIP", Name: "sip", Type: "bool", Optional: true, Default: values["sip"], Placeholder: "Разрешить звонок в конференцию по SIP"},
	)
	return model.Dialog{
		CallbackId:  "schedule",
		Title:       title,
		SubmitLabel: "Сохранить",
		State:       state,
		SourceURL:   "/plugins/" + pluginID + "/dialogs/schedule",
		Elements:    elements,
	}
}
