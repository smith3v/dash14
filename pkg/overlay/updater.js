(() => {
  const { overlayView, layoutRevision } = document.body.dataset;
  if (!layoutRevision || new URLSearchParams(location.search).get("updates") === "off") return;

  const counters = [...document.querySelectorAll("[data-score]")].map(node => ({
    node, value: node.textContent,
  }));
  const rows = [...document.querySelectorAll("[data-set]")].map(node => ({
    node,
    hidden: node.hidden,
    home: node.querySelector("[data-set-home]"),
    guest: node.querySelector("[data-set-guest]"),
  }));
  const empty = document.querySelector("[data-empty-history]");
  let emptyHidden = empty?.hidden;

  function validScore(value) {
    return Number.isSafeInteger(value) && value >= 0;
  }

  function validGame(game) {
    if (game === null) return true;
    return game && ["homePoints", "guestPoints", "homeSetsWon", "guestSetsWon"]
      .every(key => validScore(game[key])) &&
      Array.isArray(game.setScores) &&
      game.setScores.every(row => row && Number.isInteger(row.set) &&
        row.set >= 1 && row.set <= 5 && validScore(row.home) && validScore(row.guest));
  }

  function update(game) {
    for (const counter of counters) {
      const value = String(game[counter.node.dataset.score]);
      if (counter.value !== value) {
        counter.node.textContent = value;
        counter.value = value;
      }
    }

    for (const row of rows) {
      const score = game.setScores.find(entry => entry.set === Number(row.node.dataset.set));
      if (score) {
        if (row.home.textContent !== String(score.home)) row.home.textContent = score.home;
        if (row.guest.textContent !== String(score.guest)) row.guest.textContent = score.guest;
      }
      const hidden = !score;
      if (row.hidden !== hidden) {
        row.node.hidden = hidden;
        row.hidden = hidden;
      }
    }
    const hidden = game.setScores.length > 0;
    if (empty && emptyHidden !== hidden) {
      empty.hidden = hidden;
      emptyHidden = hidden;
    }
  }

  async function poll() {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 5000);
    try {
      const response = await fetch("/api/v1/game", { cache: "no-store", signal: controller.signal });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const state = await response.json();
      const revision = overlayView === "main" ? state.mainRevision : state.intermissionRevision;
      if (typeof revision !== "string" || !revision || !validGame(state.game)) {
        throw new Error("Invalid overlay state");
      }
      if (revision !== layoutRevision) {
        location.reload();
        return;
      }
      if (state.game) update(state.game);
    } catch (error) {
      console.warn("Dash14 overlay poll failed", error);
    } finally {
      clearTimeout(timeout);
    }
    setTimeout(poll, 2000);
  }

  setTimeout(poll, 2000);
})();
