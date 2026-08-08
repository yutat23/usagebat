// Progressive enhancement. Every control on the page is a form that works on
// its own; this only makes the round trip quieter, so a browser that refuses
// to run the script still gets a working settings screen.
//
// The gain is not really the spinner. A full page reload re-reads the history
// file and redraws every chart in order to change one checkbox, and it throws
// away where the reader had scrolled to.
(function () {
  "use strict";

  document.documentElement.classList.add("js");
  var busy = false;

  function markBusy(on) {
    busy = on;
    document.body.classList.toggle("busy", on);
  }

  function showStatus(message, error) {
    var status = document.getElementById("settings-status");
    if (!status) return;
    status.textContent = message || "";
    status.classList.toggle("error", !!error);
  }

  function label(name) {
    var tabs = document.querySelector(".tabbar");
    return tabs ? tabs.getAttribute("data-" + name + "-label") || "" : "";
  }

  function selectTab(id, updateHash) {
    var panels = document.querySelectorAll("[data-tab-panel]");
    var buttons = document.querySelectorAll("[data-tab-target]");
    var found = false;
    for (var i = 0; i < panels.length; i++) {
      if (panels[i].getAttribute("data-tab-panel") === id) found = true;
    }
    if (!found) id = "overview";
    for (var p = 0; p < panels.length; p++) {
      var panelActive = panels[p].getAttribute("data-tab-panel") === id;
      panels[p].classList.toggle("active", panelActive);
      panels[p].hidden = !panelActive;
    }
    for (var b = 0; b < buttons.length; b++) {
      var buttonActive = buttons[b].getAttribute("data-tab-target") === id;
      buttons[b].setAttribute("aria-selected", buttonActive ? "true" : "false");
      buttons[b].tabIndex = buttonActive ? 0 : -1;
    }
    if (updateHash && window.history && window.history.replaceState) {
      window.history.replaceState(null, "", "#" + id);
    }
  }

  function bindTabs() {
    var buttons = document.querySelectorAll("[data-tab-target]");
    var initial = window.location.hash ? window.location.hash.slice(1) : "overview";
    selectTab(initial, false);
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].addEventListener("click", function (event) {
        selectTab(event.currentTarget.getAttribute("data-tab-target"), true);
      });
      buttons[i].addEventListener("keydown", function (event) {
        if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
        event.preventDefault();
        var all = Array.prototype.slice.call(document.querySelectorAll("[data-tab-target]"));
        var at = all.indexOf(event.currentTarget);
        var direction = event.key === "ArrowRight" ? 1 : -1;
        var next = all[(at + direction + all.length) % all.length];
        next.focus();
        selectTab(next.getAttribute("data-tab-target"), true);
      });
    }
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
    showStatus("", false);
    markBusy(true);
    fetch(form.action, {
      method: "POST",
      credentials: "same-origin",
      headers: { "X-Requested-With": "fetch" },
      body: new URLSearchParams(new FormData(form)),
    })
      .then(function (response) {
        if (response.ok) return reload();
        return response.text().then(function (message) {
          throw new Error(message.trim() || response.status);
        });
      })
      .then(function () {
        markBusy(false);
        showStatus(label("saved"), false);
      })
      .catch(function (error) {
        markBusy(false);
        showStatus(label("save-error") + ": " + error.message, true);
      });
  }

  function bind() {
    var forms = document.querySelectorAll("form[data-async]");
    for (var i = 0; i < forms.length; i++) {
      forms[i].addEventListener("submit", function (event) {
        event.preventDefault();
        submit(event.currentTarget);
      });
      var fields = forms[i].querySelectorAll('input[name="value"], select[name="value"]');
      for (var j = 0; j < fields.length; j++) {
        fields[j].addEventListener("change", function (event) {
          var form = event.currentTarget.form;
          if (form && form.reportValidity()) submit(form);
        });
      }
    }
    bindTabs();
  }

  bind();
})();
