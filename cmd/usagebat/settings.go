package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/yutat23/usagebat/internal/autostart"
	"github.com/yutat23/usagebat/internal/config"
	"github.com/yutat23/usagebat/internal/i18n"
	"github.com/yutat23/usagebat/internal/model"
	"github.com/yutat23/usagebat/internal/version"
	"github.com/yutat23/usagebat/internal/webui"
)

// The settings screen is served to the browser rather than drawn as a window.
// One page covers both platforms, and the rows reuse the identifiers the tray
// menu already dispatches, so a setting has a single implementation.

// openSettings starts the local server if needed and points the browser at it.
func (a *app) openSettings() {
	url, err := a.settings.Open()
	if err != nil {
		log.Printf("settings: %v", err)
		// Losing the screen should not lose access to the settings; the file
		// is what it was editing anyway.
		if err := openPath(a.currentConfig().FilePath()); err != nil {
			log.Printf("open config: %v", err)
		}
		return
	}
	if err := openPath(url); err != nil {
		log.Printf("open browser: %v", err)
	}
}

// applySetting handles a row the user clicked on the settings page.
//
// It runs the same handler the tray menu uses, so a setting behaves
// identically wherever it is changed, and it returns only once the change has
// taken effect: the page the browser draws next has to show the new state, not
// the one it already had.
func (a *app) applySetting(id, value string) error {
	switch id {
	case idQuit:
		// The page has no quit row. Nothing that arrives over a socket should
		// be able to close the app, whatever else it got hold of.
		return nil
	case idRefresh:
		a.refreshAndWait()
		return nil
	case "profile:add":
		return a.applyEditableChange(func(c *config.Config) error { return c.AddCodexProfile() })
	case "claude-profile:add":
		return a.applyEditableChange(func(c *config.Config) error { return c.AddClaudeProfile() })
	}
	if strings.HasPrefix(id, "claude-profile:remove:") {
		index, err := strconv.Atoi(strings.TrimPrefix(id, "claude-profile:remove:"))
		if err != nil {
			return fmt.Errorf("invalid Claude profile")
		}
		return a.applyEditableChange(func(c *config.Config) error { return c.RemoveClaudeProfile(index) })
	}
	if strings.HasPrefix(id, "claude-profile:move:") {
		parts := strings.Split(strings.TrimPrefix(id, "claude-profile:move:"), ":")
		if len(parts) != 2 {
			return fmt.Errorf("invalid Claude profile move")
		}
		index, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid Claude profile move")
		}
		offset := -1
		if parts[1] == "down" {
			offset = 1
		} else if parts[1] != "up" {
			return fmt.Errorf("invalid Claude profile move")
		}
		return a.applyEditableChange(func(c *config.Config) error {
			return c.MoveClaudeProfile(index, offset)
		})
	}
	if strings.HasPrefix(id, "profile:remove:") {
		index, err := strconv.Atoi(strings.TrimPrefix(id, "profile:remove:"))
		if err != nil {
			return fmt.Errorf("invalid profile")
		}
		return a.applyEditableChange(func(c *config.Config) error { return c.RemoveCodexProfile(index) })
	}
	if strings.HasPrefix(id, "profile:move:") {
		parts := strings.Split(strings.TrimPrefix(id, "profile:move:"), ":")
		if len(parts) != 2 {
			return fmt.Errorf("invalid profile move")
		}
		index, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid profile move")
		}
		offset := -1
		if parts[1] == "down" {
			offset = 1
		} else if parts[1] != "up" {
			return fmt.Errorf("invalid profile move")
		}
		return a.applyEditableChange(func(c *config.Config) error {
			return c.MoveCodexProfile(index, offset)
		})
	}
	if strings.HasPrefix(id, "setting:") {
		key := strings.TrimPrefix(id, "setting:")
		return a.applyEditableChange(func(c *config.Config) error {
			return c.SetEditableSetting(key, value)
		})
	}
	a.onClick(id)
	return nil
}

func (a *app) applyEditableChange(change func(*config.Config) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := change(a.cfg); err != nil {
		return err
	}
	// Profiles and history options are represented by runtime objects, so keep
	// them in step with the values that were just persisted.
	a.rebuild()
	a.recorder = newRecorder(a.cfg)
	a.requestRefresh()
	return nil
}

// refreshWait bounds how long the settings page will sit on a refresh. A
// Claude reading goes out to the CLI, which has its own timeout.
const refreshWait = 30 * time.Second

// refreshAndWait asks the update loop for a collection and waits for it to
// land. Providers are only ever touched on that goroutine, so this cannot
// simply collect here; it watches for the snapshot to be replaced instead.
func (a *app) refreshAndWait() {
	before := a.snap.Load()
	a.onClick(idRefresh)

	deadline := time.Now().Add(refreshWait)
	for time.Now().Before(deadline) {
		if a.snap.Load() != before {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Timed out: the page still renders, just with what was already there.
	log.Printf("settings: refresh did not finish within %s", refreshWait)
}

// settingsPage builds the screen from the live config and the last snapshot.
// It runs per request, so the page can never show a stale value.
//
// Charts and settings share one screen, separated into focused tabs. A change
// can refresh its panel without opening another browser page.
func (a *app) settingsPage() webui.Page {
	cfg := a.currentConfig()
	snap := a.snap.Load()
	if snap == nil {
		snap = &model.Snapshot{}
	}
	p := i18n.New(cfg.LanguageSetting())
	editable := cfg.EditableSettings()
	save := p.T("save")
	input := func(key, label, value, inputType, min, max, detail string) webui.Row {
		return webui.Row{
			ID: "setting:" + key, Label: label, Value: value, Kind: webui.KindInput,
			InputType: inputType, Min: min, Max: max, Step: "1", Detail: detail,
			SubmitLabel: save,
		}
	}
	selectRow := func(key, label, value string, options []webui.Option) webui.Row {
		return webui.Row{
			ID: "setting:" + key, Label: label, Value: value, Kind: webui.KindSelect,
			Options: options, SubmitLabel: save,
		}
	}

	page := webui.Page{
		Title:          p.T("settingsTitle"),
		Version:        version.String(),
		OverviewLabel:  p.T("overview"),
		SettingsLabel:  p.T("settingsNavigation"),
		SavedLabel:     p.T("saved"),
		SaveErrorLabel: p.T("saveFailed"),
		Footer:         p.T("viewOnGitHub"),
		FooterHref:     homepageURL,
		Mascot:         mascot(p),
		Sections:       a.chartSections(cfg, snap, p),
	}
	add := func(categoryID, categoryTitle string, s webui.Section) {
		if len(s.Rows) > 0 {
			s.Aside = true
			s.CategoryID = categoryID
			s.CategoryTitle = categoryTitle
			page.Sections = append(page.Sections, s)
		}
	}
	generalID, generalTitle := "general", p.T("generalSettings")
	accountsID, accountsTitle := "accounts", p.T("accountSettings")
	dataID, dataTitle := "alerts-data", p.T("alertsAndData")
	appearanceID, appearanceTitle := "appearance", p.T("appearanceSettings")

	mode := cfg.Mode()
	icon := webui.Section{Title: p.T("iconStyle")}
	for _, m := range []config.DisplayMode{config.ModeBoth, config.ModeBattery, config.ModePercent} {
		icon.Rows = append(icon.Rows, webui.Row{
			ID: idModePfx + string(m), Label: modeTitle(m, p),
			Kind: webui.KindRadio, Group: "mode", Checked: mode == m,
		})
	}
	icon.Rows = append(icon.Rows,
		selectRow("icon.windowsLayout", p.T("windowsLayout"), editable.Icon.WindowsLayout,
			[]webui.Option{{Value: "stack", Label: p.T("layoutStack")},
				{Value: "single", Label: p.T("layoutSingle")}}),
		input("refreshSeconds", p.T("refreshInterval"), strconv.Itoa(editable.RefreshSeconds),
			"number", "5", "3600", p.T("secondsDetail")),
	)
	add(generalID, generalTitle, icon)

	selected := map[string]bool{}
	for _, id := range cfg.EnabledDisplaySources() {
		selected[id] = true
	}
	services := webui.Section{Title: p.T("servicesShown")}
	for _, source := range []struct{ id, title string }{
		{model.SourceClaudeCode, "Claude Code"},
		{model.SourceCodex, "Codex"},
	} {
		if !snapshotHasFamily(snap, source.id) {
			continue
		}
		services.Rows = append(services.Rows, webui.Row{
			ID: idSourcePfx + source.id, Label: source.title,
			Kind: webui.KindToggle, Checked: selected[source.id],
		})
	}
	add(generalID, generalTitle, services)

	for _, source := range []struct{ id, title string }{
		{model.SourceClaudeCode, p.T("claudeLimits")},
		{model.SourceCodex, p.T("codexLimits")},
	} {
		if !snapshotHasFamily(snap, source.id) {
			continue
		}
		limits := webui.Section{Title: source.title}
		auto, windows := cfg.LimitSelection(source.id)
		limits.Rows = append(limits.Rows, webui.Row{
			ID: idLimitPfx + source.id + ":auto", Label: p.T("shortest"),
			Kind: webui.KindToggle, Checked: auto,
		})
		enabled := map[model.Window]bool{}
		for _, w := range windows {
			enabled[w] = true
		}
		for _, w := range model.AllWindows {
			limits.Rows = append(limits.Rows, webui.Row{
				ID: idLimitPfx + source.id + ":" + string(w), Label: p.WindowTitle(w),
				Kind: webui.KindToggle, Checked: !auto && enabled[w], Indent: 1,
			})
		}
		add(generalID, generalTitle, limits)
	}

	language := webui.Section{Title: p.T("language")}
	current := cfg.LanguageSetting()
	for _, choice := range []struct{ id, title string }{
		{i18n.Auto, p.T("systemDefault")}, {i18n.EN, p.T("english")}, {i18n.JA, p.T("japanese")},
	} {
		language.Rows = append(language.Rows, webui.Row{
			ID: idLanguagePfx + choice.id, Label: choice.title,
			Kind: webui.KindRadio, Group: "language", Checked: current == choice.id,
		})
	}
	add(generalID, generalTitle, language)

	notifications := webui.Section{Title: p.T("notificationSettings")}
	notifications.Rows = append(notifications.Rows,
		webui.Row{ID: idNotifications, Label: p.T("notifications"),
			Kind: webui.KindToggle, Checked: editable.Notifications.BankedResetExpiry.Enabled},
		input("notifications.bankedResetExpiry.thresholdHours", p.T("expiryThresholds"),
			joinInts(editable.Notifications.BankedResetExpiry.ThresholdHours), "text", "", "", p.T("hoursListDetail")),
		webui.Row{ID: idLimitAlerts, Label: p.T("limitAlerts"),
			Detail: p.LimitAlertDetail(editable.Notifications.LimitThresholds.Percents),
			Kind:   webui.KindToggle, Checked: editable.Notifications.LimitThresholds.Enabled},
		input("notifications.limitThresholds.percents", p.T("remainingThresholds"),
			joinInts(editable.Notifications.LimitThresholds.Percents), "text", "", "", p.T("percentListDetail")),
	)
	add(dataID, dataTitle, notifications)

	historySettings := webui.Section{Title: p.T("historySettings")}
	historySettings.Rows = append(historySettings.Rows,
		webui.Row{ID: idHistory, Label: p.T("recordHistory"), Detail: p.T("historyDetail"),
			Kind: webui.KindToggle, Checked: editable.History.Enabled},
		input("history.intervalMinutes", p.T("historyInterval"), strconv.Itoa(editable.History.IntervalMinutes),
			"number", "1", "1440", p.T("minutesDetail")),
		input("history.retentionDays", p.T("historyRetention"), strconv.Itoa(editable.History.RetentionDays),
			"number", "1", "3650", p.T("daysDetail")),
	)
	add(dataID, dataTitle, historySettings)

	updateSettings := webui.Section{Title: p.T("updateSettings")}
	updateSettings.Rows = append(updateSettings.Rows,
		webui.Row{ID: idUpdateCheck, Label: p.T("checkForUpdates"), Detail: p.T("updateDetail"),
			Kind: webui.KindToggle, Checked: editable.UpdateCheck.Enabled},
		input("updateCheck.intervalHours", p.T("updateInterval"), strconv.Itoa(editable.UpdateCheck.IntervalHours),
			"number", "1", "168", p.T("hoursDetail")),
	)
	if editable.UpdateCheck.Enabled {
		if latest := a.latest.Load(); latest != nil {
			updateSettings.Rows = append(updateSettings.Rows, webui.Row{
				Label: p.UpdateAvailable(latest.Version), Kind: webui.KindAction, Href: latest.URL, Indent: 1,
			})
		} else {
			updateSettings.Rows = append(updateSettings.Rows,
				webui.Row{Label: p.T("upToDate"), Kind: webui.KindText, Indent: 1})
		}
	}
	add(dataID, dataTitle, updateSettings)

	profileGroups := []struct {
		profiles                 []config.Profile
		settingPrefix, actionID  string
		accountTitle, groupTitle string
		pathDetail               string
	}{
		{editable.ClaudeProfiles, "claude", "claude-profile", p.T("claudeAccount"), p.T("claudeAccounts"), p.T("claudeProfilePathDetail")},
		{editable.CodexProfiles, "codex", "profile", p.T("codexAccount"), p.T("codexAccounts"), p.T("profilePathDetail")},
	}
	for _, group := range profileGroups {
		for index, profile := range group.profiles {
			section := webui.Section{Title: fmt.Sprintf("%s %d", group.accountTitle, index+1)}
			prefix := fmt.Sprintf("%s.profiles.%d.", group.settingPrefix, index)
			section.Rows = append(section.Rows,
				input(prefix+"path", p.T("profilePath"), profile.Path, "text", "", "", group.pathDetail),
				input(prefix+"label", p.T("profileLabel"), profile.Label, "text", "", "", p.T("profileLabelDetail")),
				input(prefix+"short", p.T("profileShort"), profile.Short, "text", "", "", p.T("profileShortDetail")),
			)
			if len(group.profiles) > 1 {
				if index > 0 {
					section.Rows = append(section.Rows, webui.Row{
						ID: fmt.Sprintf("%s:move:%d:up", group.actionID, index), Label: p.T("moveAccountUp"), Kind: webui.KindAction,
					})
				}
				if index < len(group.profiles)-1 {
					section.Rows = append(section.Rows, webui.Row{
						ID: fmt.Sprintf("%s:move:%d:down", group.actionID, index), Label: p.T("moveAccountDown"), Kind: webui.KindAction,
					})
				}
				section.Rows = append(section.Rows, webui.Row{
					ID: fmt.Sprintf("%s:remove:%d", group.actionID, index), Label: p.T("removeAccount"), Kind: webui.KindAction,
				})
			}
			add(accountsID, accountsTitle, section)
		}
		add(accountsID, accountsTitle, webui.Section{Title: group.groupTitle, Rows: []webui.Row{{
			ID: group.actionID + ":add", Label: p.T("addAccount"), Kind: webui.KindAction,
		}}})
	}

	colorFields := []struct{ key, label string }{
		{"good", "colorGood"}, {"warn", "colorWarn"}, {"critical", "colorCritical"},
		{"unknown", "colorUnknown"}, {"claude", "colorClaude"}, {"codex", "colorCodex"},
		{"period", "colorPeriod"}, {"textOnFill", "colorTextOnFill"},
	}
	for _, theme := range []struct {
		key, title string
		colors     config.ThemeColors
	}{{"light", p.T("lightColors"), editable.Colors.Light},
		{"dark", p.T("darkColors"), editable.Colors.Dark}} {
		values := map[string]string{
			"good": theme.colors.Good, "warn": theme.colors.Warn, "critical": theme.colors.Critical,
			"unknown": theme.colors.Unknown, "claude": theme.colors.Claude, "codex": theme.colors.Codex,
			"period": theme.colors.Period, "textOnFill": theme.colors.TextOnFill,
		}
		section := webui.Section{Title: theme.title}
		for _, field := range colorFields {
			section.Rows = append(section.Rows, input("colors."+theme.key+"."+field.key,
				p.T(field.label), values[field.key], "color", "", "", ""))
		}
		add(appearanceID, appearanceTitle, section)
	}
	thresholds := webui.Section{Title: p.T("colorThresholds")}
	thresholds.Rows = append(thresholds.Rows,
		input("colors.warnBelow", p.T("warnBelow"),
			strconv.FormatFloat(editable.Colors.WarnBelow, 'f', -1, 64), "number", "1", "100", p.T("percentDetail")),
		input("colors.criticalBelow", p.T("criticalBelow"),
			strconv.FormatFloat(editable.Colors.CriticalBelow, 'f', -1, 64), "number", "1", "100", p.T("percentDetail")),
	)
	add(appearanceID, appearanceTitle, thresholds)

	var other webui.Section
	if autostart.Supported() {
		enabled, err := autostart.Enabled()
		row := webui.Row{
			ID: idAutostart, Label: p.T("launchAtStartup"),
			Kind: webui.KindToggle, Checked: enabled,
		}
		if err != nil {
			row.Detail = err.Error()
		}
		other.Rows = append(other.Rows, row)
	}
	other.Rows = append(other.Rows,
		webui.Row{ID: idRefresh, Label: p.T("refreshNow"), Kind: webui.KindAction},
		webui.Row{ID: idConfig, Label: p.T("openConfig"), Kind: webui.KindAction},
	)
	add(generalID, generalTitle, other)

	return page
}

func joinInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ", ")
}
