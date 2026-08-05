package main

import (
	"log"

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
// identically wherever it is changed, and it runs synchronously: the redirect
// that follows has to render the value that was just saved.
func (a *app) applySetting(id string) {
	if id == idQuit {
		// The page has no quit row. Nothing that arrives over a socket should
		// be able to close the app, whatever else it got hold of.
		return
	}
	a.onClick(id)
}

// settingsPage builds the screen from the live config and the last snapshot.
// It runs per request, so the page can never show a stale value.
//
// Charts and settings share one screen: they are read together, and putting
// them on separate pages meant changing a setting and then navigating back to
// see what it did.
func (a *app) settingsPage() webui.Page {
	cfg := a.currentConfig()
	snap := a.snap.Load()
	if snap == nil {
		snap = &model.Snapshot{}
	}
	p := i18n.New(cfg.LanguageSetting())

	page := webui.Page{
		Title:      p.T("settingsTitle"),
		Version:    version.String(),
		Footer:     p.T("viewOnGitHub"),
		FooterHref: homepageURL,
		Sections:   a.chartSections(cfg, snap, p),
	}
	// Settings go in the narrow column beside the charts.
	add := func(s webui.Section) {
		if len(s.Rows) > 0 {
			s.Aside = true
			page.Sections = append(page.Sections, s)
		}
	}

	mode := cfg.Mode()
	icon := webui.Section{Title: p.T("iconStyle")}
	for _, m := range []config.DisplayMode{config.ModeBoth, config.ModeBattery, config.ModePercent} {
		icon.Rows = append(icon.Rows, webui.Row{
			ID: idModePfx + string(m), Label: modeTitle(m, p),
			Kind: webui.KindRadio, Group: "mode", Checked: mode == m,
		})
	}
	add(icon)

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
	add(services)

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
		add(limits)
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
	add(language)

	var other webui.Section
	if snapshotHasFamily(snap, model.SourceCodex) {
		other.Rows = append(other.Rows, webui.Row{
			ID: idNotifications, Label: p.T("notifications"),
			Kind: webui.KindToggle, Checked: cfg.BankedResetNotifications().Enabled,
		})
	}
	other.Rows = append(other.Rows, webui.Row{
		ID: idLimitAlerts, Label: p.T("limitAlerts"), Detail: p.LimitAlertDetail(
			cfg.LimitThresholdSettings().Percents),
		Kind: webui.KindToggle, Checked: cfg.LimitThresholdSettings().Enabled,
	})
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
	other.Rows = append(other.Rows, webui.Row{
		ID: idHistory, Label: p.T("recordHistory"), Detail: p.T("historyDetail"),
		Kind: webui.KindToggle, Checked: cfg.HistorySettings().Enabled,
	})

	updates := cfg.UpdateCheckSettings()
	other.Rows = append(other.Rows, webui.Row{
		ID: idUpdateCheck, Label: p.T("checkForUpdates"), Detail: p.T("updateDetail"),
		Kind: webui.KindToggle, Checked: updates.Enabled,
	})
	if updates.Enabled {
		if latest := a.latest.Load(); latest != nil {
			other.Rows = append(other.Rows, webui.Row{
				Label: p.UpdateAvailable(latest.Version), Kind: webui.KindAction,
				Href: latest.URL, Indent: 1,
			})
		} else {
			other.Rows = append(other.Rows, webui.Row{
				Label: p.T("upToDate"), Kind: webui.KindText, Indent: 1,
			})
		}
	}

	other.Rows = append(other.Rows, webui.Row{
		ID: idConfig, Label: p.T("openConfig"), Kind: webui.KindAction,
	})
	add(other)

	return page
}
