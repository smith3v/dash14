"use strict";

const telegram = window.Telegram?.WebApp;
const initData = telegram?.initData;
const setLabel = document.querySelector("#set-label");
const teams = document.querySelector("#teams");
const options = document.querySelector("#options");
const switchSides = document.querySelector("#switch-sides");
const status = document.querySelector("#status");
const retry = document.querySelector("#retry");
const template = document.querySelector("#team-control");

let state;
let pending;
let submitting = false;
let refreshing = false;
let refreshVersion = 0;
let connected = false;
let actionRejected = false;

function teamControl(role) {
  const card = template.content.firstElementChild.cloneNode(true);
  card.classList.add(role);
  const add = card.querySelector(".add");
  const subtract = card.querySelector(".subtract");
  add.addEventListener("click", () => act("game:" + role + ":+1"));
  subtract.addEventListener("click", () => act("game:" + role + ":-1"));
  return {
    card, add, subtract,
    name: card.querySelector(".name"),
    score: card.querySelector(".score"),
  };
}

const home = teamControl("home");
const guest = teamControl("guest");
teams.append(home.card, guest.card);

function setStatus(message, error = false) {
  if (status.textContent !== message) status.textContent = message;
  status.parentElement.classList.toggle("error", error);
}

function editable() {
  return connected && state?.setId > 0 && !pending && !submitting;
}

function updateButtons() {
  const enabled = editable();
  for (const [control, team] of [[home, state?.home], [guest, state?.guest]]) {
    control.add.disabled = !enabled || state.setFinishable;
    control.subtract.disabled = !enabled || team.score === 0;
  }
  switchSides.disabled = !enabled;
  if (!enabled) options.open = false;
  retry.hidden = !pending || submitting;
}

function render(nextState) {
  state = nextState;
  connected = true;
  const active = state.setId > 0;
  setLabel.textContent = state.phase === "planned" ? "Before the match" : "Set " + state.setNumber;
  for (const [control, team] of [[home, state.home], [guest, state.guest]]) {
    control.name.textContent = team.name;
    control.score.textContent = active ? team.score : "–";
    control.add.querySelector(".increment").hidden = state.setFinishable;
    const label = team.name + ", " + team.score + " points";
    control.add.setAttribute("aria-label", state.setFinishable ? label : label + ". Add one point");
    control.subtract.setAttribute("aria-label", "Remove one point from " + team.name);
  }
  const left = state.homeSide === "right" ? guest.card : home.card;
  if (teams.firstElementChild !== left) teams.prepend(left);
  teams.hidden = false;
  options.hidden = !active;
  updateButtons();
}

function showScoreStatus(message = "") {
  if (!state.setId) {
    setStatus("Use /game for set and match controls.");
  } else if (state.setFinishable) {
    setStatus("Set ready to finish. Confirm in /game or correct the score here.");
  } else {
    setStatus(message);
  }
}

async function request(path, cmd) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 7000);
  try {
    const headers = {Authorization: "tma " + initData};
    if (cmd) headers["Content-Type"] = "application/json";
    const response = await fetch(path, {
      method: cmd ? "POST" : "GET",
      headers,
      body: cmd ? JSON.stringify(cmd) : undefined,
      signal: controller.signal,
    });
    const body = await response.json();
    return {status: response.status, body};
  } finally {
    clearTimeout(timeout);
  }
}

async function refresh() {
  if (refreshing || pending || document.hidden) return;
  refreshing = true;
  const version = refreshVersion;
  try {
    const result = await request("/scoring/state");
    if (version !== refreshVersion) return;
    if (result.status === 200) {
      const changed = !connected || state?.setId !== result.body.setId ||
        state?.setFinishable !== result.body.setFinishable;
      render(result.body);
      if (changed && (!actionRejected || !state.setId || state.setFinishable)) showScoreStatus();
    } else {
      connected = false;
      actionRejected = false;
      updateButtons();
      setStatus(result.body.error, true);
    }
  } catch {
    if (version === refreshVersion) {
      connected = false;
      actionRejected = false;
      updateButtons();
      setStatus("Unable to load the score. Reconnecting…", true);
    }
  } finally {
    refreshing = false;
  }
}

async function submit() {
  if (!pending || submitting) return;
  submitting = true;
  refreshVersion++;
  updateButtons();
  setStatus("Saving…");
  try {
    const result = await request("/scoring/actions", pending);
    if (result.status === 200) {
      const message = pending.action === "game:reverse" ? "Sides switched." : "Score saved.";
      pending = undefined;
      render(result.body);
      showScoreStatus(message);
    } else if (result.status >= 500) {
      setStatus("Could not confirm the update. Retry this action.", true);
    } else {
      pending = undefined;
      connected = false;
      actionRejected = true;
      setStatus(result.body.error, true);
    }
  } catch {
    setStatus("Could not confirm the update. Retry this action.", true);
  } finally {
    submitting = false;
    updateButtons();
  }
}

function act(action) {
  if (!editable()) return;
  actionRejected = false;
  pending = {action, gameId: state.gameId, setId: state.setId, requestId: crypto.randomUUID()};
  void submit();
}

switchSides.addEventListener("click", () => act("game:reverse"));
retry.addEventListener("click", submit);

if (initData) {
  telegram.ready();
  telegram.expand();
  document.addEventListener("visibilitychange", refresh);
  setInterval(refresh, 2000);
  void refresh();
} else {
  setStatus("Open the scoring panel from /game in Telegram.", true);
}
