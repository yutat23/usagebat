package webui

import (
	_ "embed"
	"html/template"
	"log"
	"net/http"
)

//go:embed page.html
var pageHTML string

//go:embed app.js
var appJS string

// pageTemplate is parsed once. A failure here is a bug in the shipped
// template, not something a user can cause, so it panics at startup rather
// than failing the first time somebody opens the settings.
var pageTemplate = template.Must(template.New("page").Parse(pageHTML))

func (s *Server) renderPage(w http.ResponseWriter) {
	page := Page{}
	if s.Render != nil {
		page = s.Render()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The page is built from local state and never embeds anything remote. The
	// only script it may run is the one this server serves: no inline handlers,
	// nothing from anywhere else.
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'self'; connect-src 'self'; "+
			"style-src 'unsafe-inline'; img-src 'self' data:; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if err := pageTemplate.Execute(w, page); err != nil {
		// The response is already partly written by now, so there is nothing
		// useful to send; record it and let the browser show what arrived.
		log.Printf("settings page: %v", err)
	}
}
