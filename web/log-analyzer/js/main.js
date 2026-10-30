/**
 * Application wiring: pick an instance → pick logs → let the server analyse
 * them → render the report.
 *
 * There is no parsing here. `dayzctl web` owns the parsers; this file is only
 * UI state and fetch calls.
 */

import { fetchHealth, fetchInstances, fetchLogs, analyze, fetchMapMeta, fetchMaps } from './api.js';
import { fmtInt, fmtBytes } from './format.js';
import { renderMap } from './mapview.js';
import {
  el,
  renderChips,
  renderOverview,
  renderProblems,
  renderPlayers,
  renderKills,
  renderBase,
  renderSuspicious,
  renderTimeline,
  renderMapPanel,
} from './ui.js';

const dom = {
  connect: document.getElementById('connect'),
  files: document.getElementById('files'),
  results: document.getElementById('results'),
  instances: document.getElementById('instances'),
  connectError: document.getElementById('connect-error'),
  filesTitle: document.getElementById('files-title'),
  filesDir: document.getElementById('files-dir'),
  fileList: document.getElementById('file-list'),
  fileTypeFilters: document.getElementById('file-type-filters'),
  filesEmpty: document.getElementById('files-empty'),
  refresh: document.getElementById('btn-refresh'),
  back: document.getElementById('btn-back'),
  analyzeSelected: document.getElementById('btn-analyze-sel'),
  toggleAll: document.getElementById('btn-toggle-all'),
  progress: document.getElementById('progress'),
  progressLabel: document.getElementById('progress-label'),
  progressFill: document.getElementById('progress-fill'),
  intakeError: document.getElementById('intake-error'),
  chips: document.getElementById('file-chips'),
  newAnalysis: document.getElementById('btn-new'),
  tabs: document.getElementById('tabs'),
  panels: document.getElementById('panels'),
  serverStatus: document.getElementById('server-status'),
};

const state = {
  report: null,
  instance: null,
  files: [],
  selected: new Set(),
  // "all" or a file type (RPT, ADM): servers rotate hundreds of files.
  fileTypeFilter: 'all',

  activeTab: 'overview',
  problemFilter: 'all',
  problemQuery: '',
  playerQuery: '',
  // Several players can be inspected at once.
  selectedPlayers: new Set(),
  playersOnlySelected: false,
  killQuery: '',
  killsSort: 'time',
  suspiciousFilter: 'all',
  // Time-of-day window for the timeline ("HH:MM", empty means open-ended).
  timelineFrom: '',
  timelineTo: '',
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
    background: null,
    backgroundSource: null,
    imageUrl: '',
    imageFailed: false,
    // Timeline window in epoch seconds; null means the whole analysed period.
    windowFrom: null,
    windowTo: null,
    layers: {
      tiles: true,
      names: true,
      kills: true,
      trails: false,
      builds: true,
      heat: false,
      connects: false,
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
  dom.progress.hidden = false;
  dom.progress.classList.add('indeterminate');
  dom.progressLabel.textContent = label;
  dom.progressFill.style.width = '35%';
}

function hideProgress() {
  dom.progress.hidden = true;
  dom.progress.classList.remove('indeterminate');
  dom.progressFill.style.width = '0%';
}

/* ── Instances ───────────────────────────────────────────── */

async function boot() {
  try {
    const health = await fetchHealth();
    dom.serverStatus.textContent =
      health.instances === 1 ? `dayzctl ${health.version}` : `dayzctl ${health.version} · ${health.instances} instances`;
  } catch {
    dom.serverStatus.textContent = 'API unreachable';
  }
  await loadInstances();
}

async function loadInstances() {
  hideError(dom.connectError);
  dom.instances.replaceChildren(el('p', { class: 'empty', text: 'Loading instances…' }));

  try {
    const instances = await fetchInstances();
    renderInstances(instances);
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

/* ── File selection ──────────────────────────────────────── */

async function openInstance(inst) {
  hideError(dom.connectError);
  hideError(dom.intakeError);
  state.instance = inst.name;
  state.files = [];
  state.selected = new Set();
  state.fileTypeFilter = 'all';

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
  dom.files.hidden = false;
  dom.filesTitle.textContent = `${inst.name} — choose logs`;
  dom.filesDir.textContent = inst.profilesDir;
  dom.fileList.replaceChildren(el('p', { class: 'empty', text: 'Loading log files…' }));

  try {
    const { files } = await fetchLogs(inst.name);
    state.files = files;
    state.selected = new Set(files.map((f) => f.name));
    renderFileList();
  } catch (err) {
    dom.fileList.replaceChildren();
    showError(dom.connectError, err.message);
  }
}

function renderFileList() {
  renderFileTypeFilters();

  const visible = visibleFiles();
  dom.filesEmpty.hidden = visible.length > 0;
  dom.filesEmpty.textContent = state.files.length && !visible.length
    ? `No ${state.fileTypeFilter} files in this profiles folder.`
    : 'No log files in this profiles folder yet.';

  dom.fileList.replaceChildren(...visible.map(fileRow));
  updateSelectionControls(visible);
}

/** Files matching the active type filter. */
function visibleFiles() {
  if (state.fileTypeFilter === 'all') return state.files;
  return state.files.filter((f) => String(f.type).toUpperCase() === state.fileTypeFilter);
}

function renderFileTypeFilters() {
  const counts = new Map();
  for (const f of state.files) {
    const type = String(f.type).toUpperCase();
    counts.set(type, (counts.get(type) ?? 0) + 1);
  }

  const chip = (value, label, count) =>
    el('button', {
      type: 'button',
      class: `filter${state.fileTypeFilter === value ? ' active' : ''}`,
      text: `${label} (${fmtInt(count)})`,
      'aria-pressed': state.fileTypeFilter === value ? 'true' : 'false',
      onClick: () => {
        state.fileTypeFilter = value;
        renderFileList();
      },
    });

  // Only offer types that exist, so a folder of pure RPT logs has no dead chip.
  const types = [...counts.keys()].sort((a, b) => (a === 'RPT' ? -1 : b === 'RPT' ? 1 : a.localeCompare(b)));
  dom.fileTypeFilters.replaceChildren(
    chip('all', 'All', state.files.length),
    ...types.map((type) => chip(type, type, counts.get(type))),
  );
  dom.fileTypeFilters.hidden = state.files.length === 0;
}

function fileRow(file) {
  const checkbox = el('input', {
    type: 'checkbox',
    checked: state.selected.has(file.name) ? true : null,
    onChange: (event) => {
      if (event.target.checked) state.selected.add(file.name);
      else state.selected.delete(file.name);
      updateSelectionControls();
    },
  });

  return el(
    'label',
    { class: 'file-row' },
    checkbox,
    el('span', { class: `chip-type ${String(file.type).toLowerCase()}`, text: file.type }),
    el(
      'span',
      { class: 'grow' },
      el('div', { text: file.name }),
      el('div', {
        class: 'small faint',
        text: `${fmtBytes(file.size)} · ${new Date(file.modTime).toLocaleString()}`,
      }),
    ),
  );
}

function updateSelectionControls(visible = visibleFiles()) {
  const count = state.selected.size;
  dom.analyzeSelected.disabled = count === 0;
  dom.analyzeSelected.textContent = count ? `Analyze selected (${count})` : 'Analyze selected';

  const shownSelected = visible.filter((f) => state.selected.has(f.name)).length;
  dom.toggleAll.textContent =
    visible.length > 0 && shownSelected === visible.length
      ? state.fileTypeFilter === 'all' ? 'Select none' : 'Unselect shown'
      : state.fileTypeFilter === 'all' ? 'Select all' : 'Select shown';
  dom.toggleAll.disabled = visible.length === 0;
}

dom.toggleAll.addEventListener('click', () => {
  const visible = visibleFiles();
  const allSelected = visible.length > 0 && visible.every((f) => state.selected.has(f.name));
  for (const f of visible) {
    if (allSelected) state.selected.delete(f.name);
    else state.selected.add(f.name);
  }
  renderFileList();
});

/* ── Analysis ────────────────────────────────────────────── */

async function runAnalysis() {
  if (!state.instance || state.selected.size === 0) return;

  const files = [...state.selected];
  hideError(dom.intakeError);
  showProgress(`Analysing ${files.length} file${files.length === 1 ? '' : 's'} in ${state.instance}…`);

  try {
    const report = await analyze(state.instance, files);
    state.report = report;
    resetViewState();
    hideProgress();
    showResults();
  } catch (err) {
    hideProgress();
    showError(dom.intakeError, err.message);
  }
}

dom.analyzeSelected.addEventListener('click', runAnalysis);
dom.refresh.addEventListener('click', loadInstances);
dom.back.addEventListener('click', showInstances);
dom.newAnalysis.addEventListener('click', showInstances);

function showInstances() {
  state.report = null;
  state.instance = null;
  state.mapImageUrl = '';
  state.mapId = '';
  state.instanceMapId = '';
  state.mapMeta = null;
  state.mapMetaFailed = false;
  dom.results.hidden = true;
  dom.files.hidden = true;
  dom.connect.hidden = false;
  dom.fileList.replaceChildren();
  hideProgress();
  hideError(dom.intakeError);
  window.scrollTo({ top: 0, behavior: 'smooth' });
  if (!dom.instances.childElementCount) loadInstances();
}

/* ── Results ─────────────────────────────────────────────── */

function resetViewState() {
  const report = state.report;
  state.activeTab = report.rpt && !report.adm ? 'problems' : 'overview';
  state.problemFilter = 'all';
  state.problemQuery = '';
  state.playerQuery = '';
  state.selectedPlayers = new Set();
  state.playersOnlySelected = false;
  state.killQuery = '';
  state.killsSort = 'time';
  state.suspiciousFilter = 'all';
  state.timelineFrom = '';
  state.timelineTo = '';
}

function showResults() {
  dom.connect.hidden = true;
  dom.files.hidden = true;
  dom.results.hidden = false;
  renderChips(dom.chips, state.report.files);
  setTab(state.activeTab);
  window.scrollTo({ top: 0, behavior: 'smooth' });
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
    case 'problems':
      renderProblems(panel, report, state);
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
    problems: report.rpt ? report.rpt.problemCount : 0,
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
