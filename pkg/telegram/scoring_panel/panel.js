"use strict";

const telegram = window.Telegram?.WebApp;
const initData = telegram?.initData;
const setLabel = document.querySelector("#set-label");
const teams = document.querySelector("#teams");
const options = document.querySelector("#options");
const switchSides = document.querySelector("#switch-sides");
const status = document.querySelector("#status");
const transition = document.querySelector("#transition");
const retry = document.querySelector("#retry");
const template = document.querySelector("#team-control");

let gameId = new URLSearchParams(window.location.search).get("gameId");
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
    increment: card.querySelector(".increment"),
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

function canSubmit() {
  return connected && state && !pending && !submitting;
}

function updateButtons() {
  const enabled = canSubmit();
  const scoring = enabled && state.setId > 0;
  for (const [control, team] of [[home, state?.home], [guest, state?.guest]]) {
    control.add.disabled = !scoring || state.setFinishable;
    control.subtract.disabled = !scoring || team.score === 0;
    if (state?.setId) control.increment.hidden = !connected || state.setFinishable;
    control.subtract.hidden = !connected || !state?.setId;
  }
  switchSides.disabled = !scoring;
  options.hidden = !connected || !state?.setId;
  if (!scoring) options.open = false;
  transition.hidden = !enabled || !state.transition;
  transition.textContent = state?.transition?.label || "";
  retry.hidden = !pending || submitting;
}

function render(nextState) {
  state = nextState;
  gameId = state.gameId;
  connected = true;
  const active = state.setId > 0;
  const headings = {planned: "Before the match", between_sets: "Between sets", finished: "Match finished"};
  setLabel.textContent = headings[state.phase] || "Set " + state.setNumber;
  for (const [control, team] of [[home, state.home], [guest, state.guest]]) {
    control.name.textContent = team.name;
    control.score.textContent = active ? team.score : team.setsWon;
    control.increment.textContent = active ? "+1" : "Sets won";
    control.increment.hidden = active && state.setFinishable;
    const label = active ? team.name + ", " + team.score + " points" : team.name + ", sets won: " + team.setsWon;
    control.add.setAttribute("aria-label", active && !state.setFinishable ? label + ". Add one point" : label);
    control.subtract.setAttribute("aria-label", "Remove one point from " + team.name);
  }
  const left = state.homeSide === "right" ? guest.card : home.card;
  if (teams.firstElementChild !== left) teams.prepend(left);
  teams.hidden = false;
  updateButtons();
}

function showScoreStatus(message = "") {
  if (state.phase === "between_sets") {
    setStatus("Set " + state.setNumber + " finished.");
  } else if (state.setFinishable) {
    setStatus("Set ready to finish.");
  } else {
    setStatus(message);
  }
}

function rejectState(result) {
  connected = false;
  if ([401, 403, 409].includes(result.status)) pending = undefined;
  updateButtons();
  setStatus(result.body.error, true);
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
  if (refreshing || submitting || document.hidden) return;
  refreshing = true;
  const version = refreshVersion;
  try {
    const path = gameId ? "/scoring/state?gameId=" + encodeURIComponent(gameId) : "/scoring/state";
    const result = await request(path);
    if (version !== refreshVersion) return;
    if (result.status === 200) {
      const changed = !connected || state?.phase !== result.body.phase || state?.setId !== result.body.setId ||
        state?.setFinishable !== result.body.setFinishable;
      render(result.body);
      if (!pending && changed && !actionRejected) showScoreStatus();
    } else {
      actionRejected = false;
      rejectState(result);
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
      const message = pending.revision ? "" :
        (pending.action === "game:reverse" ? "Sides switched." : "Score saved.");
      pending = undefined;
      render(result.body);
      showScoreStatus(message);
    } else if (result.status >= 500) {
      setStatus("Could not confirm the update. Retry this action.", true);
    } else {
      pending = undefined;
      actionRejected = result.status === 409;
      rejectState(result);
    }
  } catch {
    setStatus("Could not confirm the update. Retry this action.", true);
  } finally {
    submitting = false;
    updateButtons();
  }
}

function act(action, revision) {
  if (!canSubmit() || (!revision && !state.setId)) return;
  actionRejected = false;
  pending = {action, revision, gameId: state.gameId, setId: state.setId, requestId: crypto.randomUUID()};
  void submit();
}

transition.addEventListener("click", () => {
  if (state?.transition) act(state.transition.action, state.transition.revision);
});
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
