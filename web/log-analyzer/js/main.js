/**
 * Application wiring: pick an instance → let the server analyse its ADM logs
 * → render the report.
 *
 * There is no parsing here. `dayzctl web` owns the parser; this file is only
 * UI state and fetch calls.
 */

import { fetchHealth, fetchInstances, fetchLogs, analyze, fetchMapMeta, fetchMaps } from './api.js';
import { fmtInt } from './format.js';
import { renderMap } from './mapview.js';
import {
  el,
  renderOverview,
  renderPlayers,
  renderKills,
  renderBase,
  renderSuspicious,
  renderTimeline,
  renderMapPanel,
} from './ui.js';

const dom = {
  connect: document.getElementById('connect'),
  loading: document.getElementById('loading'),
  loadingLabel: document.getElementById('loading-label'),
  results: document.getElementById('results'),
  instances: document.getElementById('instances'),
  connectError: document.getElementById('connect-error'),
  intakeError: document.getElementById('intake-error'),
  refresh: document.getElementById('btn-refresh'),
  scope: document.getElementById('analysis-scope'),
  reload: document.getElementById('btn-reload'),
  newAnalysis: document.getElementById('btn-new'),
  tabs: document.getElementById('tabs'),
  panels: document.getElementById('panels'),
  serverStatus: document.getElementById('server-status'),
  whoami: document.getElementById('whoami'),
  logoutForm: document.getElementById('logout-form'),
};

const state = {
  report: null,
  // Instance name (what the API takes) and the entry from /api/instances.
  instance: null,
  instanceInfo: null,

  activeTab: 'overview',
  playerQuery: '',
  // Several players can be inspected at once.
  selectedPlayers: new Set(),
  playersOnlySelected: false,
  // RCON actions sent from this page, keyed by player key. Kept per report so a
  // kicked/banned player stops reading as "online".
  playerNotices: new Map(),
  // Live RCON ban list: "Unban" is only offered for players that carry one.
  bans: [],
  bansLoaded: false,
  bansLoading: false,
  bansError: '',
  killQuery: '',
  killsSort: 'time',
  suspiciousFilter: 'all',
  // Time-of-day window for the timeline ("HH:MM", empty means open-ended).
  timelineFrom: '',
  timelineTo: '',
  // One-line description of the map's default time window, shown above the tabs.
  windowHint: '',
  mapImageUrl: '',
  // Community map (tiles + place names) for the selected instance.
  mapId: '',
  // The map the instance is configured with; the Map tab can switch the view to
  // any other installed map without touching the configuration.
  instanceMapId: '',
  availableMaps: [],
  mapsLoaded: false,
  mapMeta: null,
  mapMetaFailed: false,
  mapMetaLoading: false,
  map: {
    // 0 means "use the selected map's own extent".
    worldSize: 0,
    // Zoom 1 fills the canvas (a wide panel gets a wide map); zooming out as
    // far as the fit scale shows the whole map at once.
    zoom: 1,
    centerX: null,
    centerY: null,
    background: null,
    backgroundSource: null,
    imageUrl: '',
    imageFailed: false,
    // Timeline window in epoch seconds; null means the whole analysed period.
    windowFrom: null,
    windowTo: null,
    layers: {
      kills: true,
      deaths: false,
      trails: true,
      builds: true,
      heat: false,
      connects: false,
      logins: false,
      jumps: true,
    },
  },
};

state.actions = {
  setState(patch) {
    for (const [key, value] of Object.entries(patch)) {
      if (key === 'json') continue;
      state[key] = value;
    }
    renderActivePanel();
  },
  goTab(tab, patch = {}) {
    setTab(tab);
    state.actions.setState(patch);
  },
};

let currentMapCanvas = null;
let resizeTimer = null;
let lastFocus = null;

/* ── Feedback ────────────────────────────────────────────── */

function showError(node, message) {
  node.hidden = false;
  node.textContent = message;
}

function hideError(node) {
  node.hidden = true;
  node.textContent = '';
}

function showProgress(label) {
  dom.loadingLabel.textContent = label;
  dom.loading.hidden = false;
}

function hideProgress() {
  dom.loading.hidden = true;
}

/* ── Instances ───────────────────────────────────────────── */

async function boot() {
  try {
    const health = await fetchHealth();
    dom.serverStatus.textContent =
      health.instances === 1 ? `dayzctl ${health.version}` : `dayzctl ${health.version} · ${health.instances} instances`;
    // The login switch is on, so offer a way out of the session.
    if (health.auth) {
      dom.logoutForm.hidden = false;
      if (health.user) {
        dom.whoami.textContent = health.user;
        dom.whoami.title = health.via === 'steam' ? 'Signed in with Steam' : 'Signed in with the admin password';
        dom.whoami.hidden = false;
      }
    }
  } catch {
    dom.serverStatus.textContent = 'API unreachable';
  }
  await loadInstances({ autoOpen: true });
}

async function loadInstances({ autoOpen = false } = {}) {
  hideError(dom.connectError);
  dom.instances.replaceChildren(el('p', { class: 'empty', text: 'Loading instances…' }));

  try {
    const instances = await fetchInstances();
    renderInstances(instances);
    // A single instance means there is nothing to choose: open it and start
    // analysing right away.
    if (autoOpen && instances.length === 1 && !instances[0].error) {
      await openInstance(instances[0]);
    }
  } catch (err) {
    dom.instances.replaceChildren();
    showError(dom.connectError, err.message);
  }
}

function renderInstances(instances) {
  if (!instances.length) {
    dom.instances.replaceChildren(
      el('p', { class: 'empty', text: 'No instances configured in /etc/dayzctl/config.yaml.' }),
    );
    return;
  }

  const cards = instances.map((inst) => {
    const meta = [];
    meta.push(`${fmtInt(inst.logFiles)} log file${inst.logFiles === 1 ? '' : 's'}`);
    if (inst.latest) meta.push(`latest ${new Date(inst.latest).toLocaleString()}`);

    return el(
      'button',
      { type: 'button', class: 'instance-card', onClick: () => openInstance(inst) },
      el('div', { class: 'name', text: inst.name }),
      el('div', { class: 'meta', text: meta.join(' · ') }),
      inst.enabled ? null : el('div', { class: 'meta warn', text: 'disabled in config' }),
      inst.error ? el('div', { class: 'meta warn', text: inst.error }) : null,
    );
  });

  dom.instances.replaceChildren(...cards);
}

/* ── Analysis ────────────────────────────────────────────── */

/**
 * Open an instance and analyse its logs. There is no file picker: every ADM log
 * in the profiles folder is parsed, and the views narrow the result down
 * afterwards (the map opens on the last 6 h).
 */
async function openInstance(inst) {
  hideError(dom.connectError);
  hideError(dom.intakeError);
  state.instance = inst.name;
  state.instanceInfo = inst;

  // A map image served for this instance is picked up by the map tab.
  state.mapImageUrl = inst.mapImage || '';
  state.map.imageUrl = '';
  state.map.imageFailed = false;
  if (state.map.backgroundSource === 'server') {
    state.map.background = null;
    state.map.backgroundSource = null;
  }

  // Community map for tiles and place names (defaults to the instance name).
  state.instanceMapId = inst.map || '';
  state.mapId = state.instanceMapId;
  state.mapMeta = null;
  state.mapMetaFailed = false;
  state.mapMetaLoading = false;
  state.map.worldSize = 0;

  if (!state.mapsLoaded) {
    fetchMaps()
      .then((maps) => {
        state.availableMaps = maps;
        state.mapsLoaded = true;
        if (state.activeTab === 'map') state.actions.setState({});
      })
      .catch(() => {
        state.availableMaps = [];
      });
  }

  dom.connect.hidden = true;
  dom.results.hidden = true;
  await analyseInstance(inst);
}

/** The analyser only understands the ADM admin log. */
function isAnalysable(file) {
  return String(file.type).toUpperCase() === 'ADM';
}

/** Send only the ADM file names. */
async function analyseInstance(inst) {
  if (!state.instance) return;

  hideError(dom.intakeError);
  dom.connect.hidden = true;
  dom.results.hidden = true;
  showProgress(`Reading the profiles folder of ${inst.name}…`);

  try {
    const { files } = await fetchLogs(inst.name);
    const wanted = files.filter((file) => isAnalysable(file));
    if (!wanted.length) {
      throw new Error(`No ADM log in ${inst.profilesDir || 'this profiles folder'}.`);
    }

    showProgress(`Analysing ${wanted.length} ADM log${wanted.length === 1 ? '' : 's'} in ${inst.name}…`);

    const report = await analyze(inst.name, wanted.map((file) => file.name));
    state.report = report;
    resetViewState();
    hideProgress();
    showResults();
  } catch (err) {
    hideProgress();
    dom.results.hidden = true;
    dom.connect.hidden = false;
    showError(dom.intakeError, err.message);
  }
}

function showInstances() {
  state.report = null;
  state.instance = null;
  state.instanceInfo = null;
  state.mapImageUrl = '';
  state.mapId = '';
  state.instanceMapId = '';
  state.mapMeta = null;
  state.mapMetaFailed = false;
  dom.results.hidden = true;
  dom.connect.hidden = false;
  hideProgress();
  hideError(dom.intakeError);
  window.scrollTo({ top: 0, behavior: 'smooth' });
  if (!dom.instances.childElementCount) loadInstances();
}

dom.reload.addEventListener('click', () => {
  if (state.instanceInfo) openInstance(state.instanceInfo);
});
dom.refresh.addEventListener('click', () => loadInstances());
dom.newAnalysis.addEventListener('click', showInstances);

/* ── Results ─────────────────────────────────────────────── */

/** How much of the analysed period the map shows when a report opens. */
const DEFAULT_WINDOW_SECONDS = 6 * 3600;

function resetViewState() {
  const report = state.report;
  state.activeTab = 'overview';
  state.playerQuery = '';
  state.selectedPlayers = new Set();
  state.playersOnlySelected = false;
  state.playerNotices = new Map();
  state.bans = [];
  state.bansLoaded = false;
  state.bansLoading = false;
  state.bansError = '';
  state.killQuery = '';
  state.killsSort = 'time';
  state.suspiciousFilter = 'all';
  state.timelineFrom = '';
  state.timelineTo = '';

  // A rotated profiles folder can span weeks, so the map's Timeline opens on
  // the last six hours instead of the whole period. Shorter logs start at
  // their own beginning; the "Whole period" button undoes it.
  const period = report.adm?.period;
  const canTrim =
    period?.hasTime &&
    Number.isFinite(period.from) &&
    Number.isFinite(period.to) &&
    period.to - period.from > DEFAULT_WINDOW_SECONDS;
  state.map.windowFrom = canTrim ? period.to - DEFAULT_WINDOW_SECONDS : null;
  state.map.windowTo = canTrim ? period.to : null;
  state.windowHint = canTrim ? 'map opens on the last 6 hours' : 'map shows the whole period';
}

function showResults() {
  dom.connect.hidden = true;
  dom.results.hidden = false;
  hideProgress();
  dom.scope.textContent = scopeText(state.report);
  setTab(state.activeTab);
  window.scrollTo({ top: 0, behavior: 'smooth' });
}

/** "server-1 · 12 ADM · map opens on the last 6 hours" */
function scopeText(report) {
  const adm = (report.files ?? []).filter((file) => String(file.type).toUpperCase() === 'ADM').length;
  return [state.instance, `${fmtInt(adm)} ADM`, state.windowHint].filter(Boolean).join(' · ');
}

function setTab(tab) {
  state.activeTab = tab;
  for (const btn of dom.tabs.querySelectorAll('.tab')) {
    btn.classList.toggle('active', btn.dataset.tab === tab);
  }
  for (const panel of dom.panels.querySelectorAll('.panel')) {
    panel.classList.toggle('active', panel.dataset.panel === tab);
  }
  renderActivePanel();
  if (tab === 'map') revealMapCard();
}

// The map is sized to the window, so bring the whole card into view instead of
// leaving half of it below the fold.
function revealMapCard() {
  const card = dom.panels.querySelector('.panel.active .card');
  if (!card) return;
  const rect = card.getBoundingClientRect();
  if (rect.top < 0 || rect.bottom > window.innerHeight - 4) {
    // Instant, not smooth: the card is sized to the viewport, so it must be in
    // place before the map is drawn, and smooth scrolling is paused in
    // background tabs.
    card.scrollIntoView({ block: 'start' });
  }
}

function panelFor(tab) {
  return dom.panels.querySelector(`.panel[data-panel="${tab}"]`);
}

function captureFocus() {
  const active = document.activeElement;
  if (!active?.closest || !active.closest('#panels')) return;
  const holder = active.closest('[data-focus]');
  if (!holder) return;
  lastFocus = { id: holder.dataset.focus, start: holder.selectionStart ?? null, end: holder.selectionEnd ?? null };
}

function restoreFocus() {
  if (!lastFocus) return;
  const target = dom.panels.querySelector(`[data-focus="${lastFocus.id}"]`);
  if (!target) return;
  target.focus();
  if (lastFocus.start !== null && typeof target.setSelectionRange === 'function') {
    try {
      target.setSelectionRange(lastFocus.start, lastFocus.end ?? lastFocus.start);
    } catch {
      /* some input types do not support selection */
    }
  }
}

function renderActivePanel() {
  const report = state.report;
  if (!report) return;

  const panel = panelFor(state.activeTab);
  if (!panel) return;

  captureFocus();
  currentMapCanvas = null;

  switch (state.activeTab) {
    case 'overview':
      renderOverview(panel, report, state.actions);
      break;
    case 'players':
      renderPlayers(panel, report, state);
      break;
    case 'kills':
      renderKills(panel, report, state);
      break;
    case 'base':
      renderBase(panel, report);
      break;
    case 'suspicious':
      renderSuspicious(panel, report, state);
      break;
    case 'timeline':
      renderTimeline(panel, report, state);
      break;
    case 'map':
      renderMapPanel(panel, report, state, () => {
        currentMapCanvas = panel.querySelector('#map-canvas');
      });
      break;
    default:
      break;
  }

  updateTabBadges(report);
  restoreFocus();
}

function updateTabBadges(report) {
  const badges = {
    players: report.adm ? report.adm.totals.players : 0,
    suspicious: report.adm ? report.adm.totals.suspicious : 0,
  };

  for (const btn of dom.tabs.querySelectorAll('.tab')) {
    const count = badges[btn.dataset.tab];
    btn.querySelector('.count')?.remove();
    if (count) btn.append(el('span', { class: 'count', text: fmtInt(count) }));
  }
}

dom.tabs.addEventListener('click', (event) => {
  const btn = event.target.closest('.tab');
  if (btn) setTab(btn.dataset.tab);
});

window.addEventListener('resize', () => {
  if (state.activeTab !== 'map' || !currentMapCanvas || !state.report) return;
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(
    () => renderMap(currentMapCanvas, state.report, state.lastMapView || state.map),
    120,
  );
});

boot();
