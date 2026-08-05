// Progressive enhancement. Every control on the page is a form that works on
// its own; this only makes the round trip quieter, so a browser that refuses
// to run the script still gets a working settings screen.
//
// The gain is not really the spinner. A full page reload re-reads the history
// file and redraws every chart in order to change one checkbox, and it throws
// away where the reader had scrolled to.
(function () {
  "use strict";

  var busy = false;

  function markBusy(on) {
    busy = on;
    document.body.classList.toggle("busy", on);
  }

  // Swap in a freshly rendered copy, keeping the scroll position: a settings
  // screen that jumps to the top on every click is worse than one that waits.
  function reload() {
    return fetch(window.location.pathname, {
      credentials: "same-origin",
      headers: { "X-Requested-With": "fetch" },
    })
      .then(function (response) {
        if (!response.ok) throw new Error(response.status);
        return response.text();
      })
      .then(function (html) {
        var next = new DOMParser().parseFromString(html, "text/html");
        var fresh = next.querySelector("main");
        var here = document.querySelector("main");
        if (fresh && here) {
          here.replaceWith(fresh);
        }
        bind();
      });
  }

  function submit(form) {
    if (busy) {
      return;
    }
    markBusy(true);
    fetch(form.action, {
      method: "POST",
      credentials: "same-origin",
      headers: { "X-Requested-With": "fetch" },
      body: new URLSearchParams(new FormData(form)),
    })
      .then(function (response) {
        if (!response.ok) throw new Error(response.status);
        return reload();
      })
      .then(function () {
        markBusy(false);
      })
      .catch(function () {
        // Whatever went wrong, the plain form still works: let the browser do
        // it the ordinary way rather than leave the click unanswered.
        markBusy(false);
        form.submit();
      });
  }

  function bind() {
    var forms = document.querySelectorAll("form[data-async]");
    for (var i = 0; i < forms.length; i++) {
      forms[i].addEventListener("submit", function (event) {
        event.preventDefault();
        submit(event.currentTarget);
      });
    }
  }

  bind();
})();
