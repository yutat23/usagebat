package webui

import (
	_ "embed"
	"html/template"
	"log"
	"net/http"
)

//go:embed page.html
var pageHTML string

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
	// The page is built from local state, never embeds anything remote, and
	// carries no script: every control is a form submission.
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; img-src 'self' data:; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if err := pageTemplate.Execute(w, page); err != nil {
		// The response is already partly written by now, so there is nothing
		// useful to send; record it and let the browser show what arrived.
		log.Printf("settings page: %v", err)
	}
}
