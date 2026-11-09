package main

import (
	"fmt"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

const alertInterval = 6 * time.Hour

var alertDays = []int{14, 7, 1}

// runAlerts warns administrators before the key or the tariff runs out.
func (p *Plugin) runAlerts() {
	client := p.client()
	if client == nil {
		return
	}
	ctx, cancel := apiContext()
	defer cancel()

	if info, err := client.GetAccessInfo(ctx); err != nil {
		p.noteAPIError(err)
		p.API.LogWarn("Alerts: key check failed", "error", err.Error())
	} else {
		p.alertExpiry("key", info.ExpiredAt, "Срок действия API-ключа Контур.Толка заканчивается %s. После этого встречи перестанут создаваться через API.")
	}
	if tariff, err := client.GetTariffExpiration(ctx); err != nil {
		p.API.LogDebug("Alerts: tariff check failed", "error", err.Error())
	} else if tariff != nil {
		p.alertExpiry("tariff", *tariff, "Тариф Контур.Толка заканчивается %s.")
	}

	if p.getConfig().EnableWebhooks {
		var state webhookState
		if found, err := p.kvGet(keyWebhook, &state); err == nil && found && !state.Activated && time.Since(state.CreatedAt) > 10*time.Minute {
			p.alertAdmins("webhook", 24*time.Hour, "Вебхук Контур.Толка не активирован: Толк не смог доставить ключ активации на адрес Mattermost. Статусы встреч обновляются опросом API.")
		}
	}
}

func (p *Plugin) alertExpiry(name string, at time.Time, template string) {
	if at.IsZero() {
		return
	}
	left := time.Until(at)
	for _, days := range alertDays {
		if left <= time.Duration(days)*24*time.Hour {
			p.alertAdmins(fmt.Sprintf("%s:%d:%s", name, days, at.Format("20060102")), 30*24*time.Hour, fmt.Sprintf(template, at.Format("02.01.2006")))
			return
		}
	}
}

// alertAdmins sends a message to system administrators at most once per period.
func (p *Plugin) alertAdmins(key string, period time.Duration, message string) {
	first, err := p.kvClaim(keyAlert+key, period)
	if err != nil || !first {
		return
	}
	admins, appErr := p.API.GetUsers(&model.UserGetOptions{Role: model.SystemAdminRoleId, Page: 0, PerPage: 50})
	if appErr != nil {
		p.API.LogWarn("Alerts: failed to list administrators", "error", appErr.Error())
		return
	}
	for _, admin := range admins {
		if !admin.IsBot && admin.DeleteAt == 0 {
			p.dmUser(admin.Id, "⚠️ "+message)
		}
	}
}
