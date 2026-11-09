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
* ` + "`/talk room`" + ` ваша постоянная комната.
* ` + "`/talk schedule`" + ` запланировать встречу.
* ` + "`/talk pin <код|off>`" + ` задать или снять ПИН-код.
* ` + "`/talk guests <часы|off>`" + ` открыть или закрыть вход для внешних участников.
* ` + "`/talk lobby <on|off>`" + ` включить или выключить зал ожидания.
* ` + "`/talk record`" + ` начать запись.
* ` + "`/talk end`" + ` завершить встречу для всех.
* ` + "`/talk connect`" + ` подключить личный API-ключ Толка, ` + "`/talk disconnect`" + ` отключить его.
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
	add("room", "", "Моя постоянная комната")
	add("schedule", "", "Запланировать встречу")
	add("pin", "<код|off>", "Задать или снять ПИН-код")
	add("guests", "<часы|off>", "Вход для внешних участников")
	lobby := add("lobby", "", "Зал ожидания")
	lobby.AddStaticListArgument("Состояние", true, []model.AutocompleteListItem{{Item: "on", HelpText: "Включить"}, {Item: "off", HelpText: "Выключить"}})
	add("record", "", "Начать запись")
	add("end", "", "Завершить встречу для всех")
	add("connect", "", "Подключить личный API-ключ")
	add("disconnect", "", "Отключить личный API-ключ")
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
		return p.commandStart(rest, args)
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
		return ephemeral("Личный ключ Толка отключён. Календарь работает через ключ пространства."), nil
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

func (p *Plugin) commandStart(title string, args *model.CommandArgs) (*model.CommandResponse, error) {
	if p.userPref(args.UserId, prefNaming, p.getConfig().NamingScheme) == schemeAsk {
		return p.openStartDialog(title, args)
	}
	m, err := p.startMeeting(args.UserId, createRequest{ChannelID: args.ChannelId, RootID: args.RootId, Title: title})
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
		if err := p.startRecording(args.UserId, m.ID); err != nil {
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

func (p *Plugin) openStartDialog(title string, args *model.CommandArgs) (*model.CommandResponse, error) {
	dialog := model.Dialog{
		CallbackId:  "start",
		Title:       "Встреча",
		SubmitLabel: "Начать",
		State:       args.RootId,
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
	if !p.getConfig().AllowPersonalKeys {
		return ephemeral("Личные API-ключи отключены администратором."), nil
	}
	if p.client() == nil {
		return ephemeral("Сначала администратор должен указать API-ключ пространства."), nil
	}
	dialog := model.Dialog{
		CallbackId:       "connect",
		Title:            "Личный ключ",
		IntroductionText: "В Толке откройте **Настройки → API ключи → Выпустить ключ** и вставьте ключ сюда. С ним встречи планируются от вашего имени, а на приглашения можно отвечать прямо в Mattermost. Ключ хранится в зашифрованном виде.",
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
	if _, err := p.requireClient(); err != nil {
		return nil, err
	}
	// `/talk schedule edit <id>` opens the same dialog for an existing meeting.
	fields := strings.Fields(rest)
	if len(fields) == 2 && fields[0] == "edit" {
		m, err := p.controlledMeeting(args.UserId, fields[1])
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
	options := func(pairs ...string) []*model.PostActionOptions {
		out := make([]*model.PostActionOptions, 0, len(pairs)/2)
		for i := 0; i+1 < len(pairs); i += 2 {
			out = append(out, &model.PostActionOptions{Text: pairs[i], Value: pairs[i+1]})
		}
		return out
	}
	values := map[string]string{"duration": "60", "repeat": "none", "lobby": "false", "guests": "false", "auto_record": "false", "sip": "false"}
	state, title := "new:"+rootID, "Планирование"
	if m != nil && m.Schedule != nil {
		sc := m.Schedule
		state, title = "edit:"+m.ID, "Изменение"
		values["title"] = m.Title
		values["start"] = sc.Start.UTC().Format(time.RFC3339)
		values["duration"] = strconv.Itoa(int(sc.End.Sub(sc.Start).Minutes()))
		values["description"] = sc.Description
		if sc.Repeat != "" {
			values["repeat"] = sc.Repeat
			values["repeat_count"] = strconv.Itoa(sc.RepeatCount)
		}
		values["participants"] = participants
		values["external"] = strings.Join(sc.ExternalEmails, ", ")
		values["pin"] = m.Pin
		values["lobby"] = strconv.FormatBool(m.Lobby)
		values["guests"] = strconv.FormatBool(sc.AllowGuests)
		values["auto_record"] = strconv.FormatBool(sc.AutoRecord)
		values["sip"] = strconv.FormatBool(sc.EnableSIP)
	}
	return model.Dialog{
		CallbackId:  "schedule",
		Title:       title,
		SubmitLabel: "Сохранить",
		State:       state,
		Elements: []model.DialogElement{
			{DisplayName: "Тема", Name: "title", Type: "text", Default: values["title"], MaxLength: maxTitleLength},
			{DisplayName: "Начало", Name: "start", Type: "datetime", Default: values["start"],
				DateTimeConfig: &model.DialogDateTimeConfig{TimeInterval: 15, LocationTimezone: timezone, MinDate: "today"}},
			{DisplayName: "Длительность", Name: "duration", Type: "select", Default: values["duration"],
				Options: options("15 минут", "15", "30 минут", "30", "45 минут", "45", "1 час", "60", "1,5 часа", "90", "2 часа", "120", "3 часа", "180")},
			// Mattermost 11 drops the preselected users of a "users" select,
			// so an edit would silently remove everyone. A dynamic select keeps
			// them, and the plugin answers its searches.
			{DisplayName: "Участники", Name: "participants", Type: "select", DataSource: "dynamic", DataSourceURL: "/plugins/" + pluginID + "/dialogs/users",
				MultiSelect: true, Optional: true, Default: values["participants"], Placeholder: "Начните вводить имя"},
			{DisplayName: "Внешние", Name: "external", Type: "text", Optional: true, Default: values["external"], MaxLength: 1000,
				Placeholder: "Адреса почты через запятую"},
			{DisplayName: "Описание", Name: "description", Type: "textarea", Optional: true, Default: values["description"], MaxLength: 2000},
			{DisplayName: "Повторение", Name: "repeat", Type: "select", Default: values["repeat"],
				Options: options("Не повторять", "none", "Ежедневно", "daily", "Еженедельно", "weekly", "Ежемесячно", "monthly")},
			{DisplayName: "Число встреч", Name: "repeat_count", Type: "text", SubType: "number", Optional: true, Default: values["repeat_count"], HelpText: "Для повторяющейся встречи: от 2 до 100."},
			{DisplayName: "ПИН-код", Name: "pin", Type: "text", Optional: true, Default: values["pin"], MaxLength: 6, Placeholder: "4–6 цифр"},
			{DisplayName: "Зал ожидания", Name: "lobby", Type: "bool", Optional: true, Default: values["lobby"], Placeholder: "Впускать участников по запросу"},
			{DisplayName: "Гости", Name: "guests", Type: "bool", Optional: true, Default: values["guests"], Placeholder: "Разрешить вход по ссылке без аккаунта"},
			{DisplayName: "Автозапись", Name: "auto_record", Type: "bool", Optional: true, Default: values["auto_record"], Placeholder: "Записывать встречу автоматически"},
			{DisplayName: "SIP", Name: "sip", Type: "bool", Optional: true, Default: values["sip"], Placeholder: "Разрешить звонок в конференцию по SIP"},
		},
	}
}
