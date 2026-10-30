/**
 * View layer. Pure DOM building — no framework, no innerHTML for log content
 * (log lines are untrusted text and are always set via textContent).
 */

import { fmtInt, fmtDuration, fmtTime, fmtClock, fmtDistance, fmtBytes, truncate, colorFor } from './format.js';
import { SEVERITY, SEVERITY_LABEL, SEVERITY_ORDER } from './severity.js';
import { renderMap, inferWorldSize, clampView, MAX_MAP_ZOOM } from './mapview.js';
import { fetchMapMeta } from './api.js';

/** Kill count per weapon, used by the overview card. */
function topWeapons(kills, limit = 8) {
  const counts = new Map();
  for (const kill of kills) {
    const name = kill.weapon || 'unknown';
    counts.set(name, (counts.get(name) ?? 0) + 1);
  }
  return [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, limit);
}

export function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (value === null || value === undefined || value === false) continue;
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key === 'dataset') Object.assign(node.dataset, value);
    else if (key.startsWith('on') && typeof value === 'function') {
      node.addEventListener(key.slice(2).toLowerCase(), value);
    } else node.setAttribute(key, value === true ? '' : value);
  }
  for (const child of children.flat(2)) {
    if (child === null || child === undefined || child === false) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
}

export function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
}

function kpi(label, value, sub, tone) {
  return el(
    'div',
    { class: `kpi${tone ? ` ${tone}` : ''}` },
    el('div', { class: 'label', text: label }),
    el('div', { class: 'value', text: value }),
    sub ? el('div', { class: 'sub', text: sub }) : null,
  );
}

function sevPill(severity) {
  return el('span', { class: `sev ${severity}`, text: SEVERITY_LABEL[severity] ?? severity });
}

function table(headers, rows) {
  const thead = el(
    'thead',
    {},
    el('tr', {}, headers.map((h) => el('th', { class: h.num ? 'num' : undefined, text: h.label }))),
  );
  const tbody = el('tbody', {}, rows);
  return el('div', { class: 'table-wrap' }, el('table', {}, thead, tbody));
}

function emptyState(text) {
  return el('div', { class: 'empty', text });
}

function card(title, ...children) {
  const body = children.filter(Boolean);
  return el('div', { class: 'card' }, title ? el('h3', { text: title }) : null, ...body);
}

/* ── File chips ──────────────────────────────────────────── */

export function renderChips(container, files) {
  clear(container);
  for (const f of files) {
    container.append(
      el(
        'div',
        { class: 'chip', title: f.error ? `${f.name} — ${f.error}` : `${f.name} · ${fmtInt(f.lines)} lines` },
        el('span', { class: `chip-type ${String(f.type).toLowerCase()}`, text: f.type }),
        el('span', { class: 'chip-name', text: f.name }),
        el('span', { class: 'chip-meta', text: f.error ? 'skipped' : `${fmtInt(f.lines)} lines` }),
      ),
    );
  }
}

/* ── Overview ────────────────────────────────────────────── */

export function renderOverview(panel, report, actions) {
  clear(panel);
  const { adm, rpt, files } = report;

  // Problems
  if (rpt) {
    const counts = rpt.counts;
    const kpis = el(
      'div',
      { class: 'kpis' },
      ...SEVERITY_ORDER.map((sev) =>
        el(
          'button',
          {
            class: 'kpi',
            style: 'text-align:left;cursor:pointer;font:inherit;color:inherit',
            onClick: () => actions.goTab('problems', { problemFilter: sev }),
          },
          el('div', { class: 'label', text: SEVERITY_LABEL[sev] }),
          el('div', {
            class: `value ${counts[sev] ? (sev === SEVERITY.INFO ? '' : 'bad') : ''}`,
            text: fmtInt(counts[sev]),
          }),
          el('div', { class: 'sub', text: counts[sev] === 1 ? 'occurrence' : 'occurrences' }),
        ),
      ),
    );

    const headline =
      rpt.problemCount === 0
        ? 'No problems found. Only harmless standard messages.'
        : `${fmtInt(rpt.problemCount)} message${rpt.problemCount === 1 ? '' : 's'} worth reviewing, in ${fmtInt(rpt.problemGroupCount)} distinct pattern${rpt.problemGroupCount === 1 ? '' : 's'}.`;

    panel.append(card('Problems found', el('p', { class: 'muted small', text: headline }), kpis));
  } else {
    panel.append(card('Problems found', emptyState('No RPT loaded — drop an .RPT file to see error analysis.')));
  }

  // Activity
  if (adm && adm.totals.players > 0) {
    const p = adm.period;
    const periodText = p.hasTime
      ? `${fmtClock(p.from)} – ${fmtClock(p.to)}`
      : 'no timestamps';
    const kpis = el(
      'div',
      { class: 'kpis' },
      kpi('Period', p.hasTime ? fmtDuration(p.seconds) : '—', p.hasTime ? periodText : null),
      el(
        'button',
        { class: 'kpi', style: 'text-align:left;font:inherit;color:inherit;cursor:pointer', onClick: () => actions.goTab('players') },
        el('div', { class: 'label', text: 'Players' }),
        el('div', { class: 'value', text: fmtInt(adm.totals.players) }),
        el('div', { class: 'sub', text: `Most online: ${fmtInt(adm.peakOnline)}` }),
      ),
      el(
        'button',
        { class: 'kpi', style: 'text-align:left;font:inherit;color:inherit;cursor:pointer', onClick: () => actions.goTab('kills') },
        el('div', { class: 'label', text: 'Deaths' }),
        el('div', { class: 'value', text: fmtInt(adm.totals.deaths) }),
        el('div', { class: 'sub', text: `PvP kills: ${fmtInt(adm.totals.pvpKills)}` }),
      ),
      el(
        'button',
        {
          class: `kpi ${adm.totals.suspicious ? 'warn' : ''}`,
          style: 'text-align:left;font:inherit;color:inherit;cursor:pointer',
          onClick: () => actions.goTab('suspicious'),
        },
        el('div', { class: 'label', text: 'Suspicious events' }),
        el('div', { class: 'value', text: fmtInt(adm.totals.suspicious) }),
      ),
      kpi('Chat messages', fmtInt(adm.totals.chats)),
      el(
        'button',
        { class: 'kpi', style: 'text-align:left;font:inherit;color:inherit;cursor:pointer', onClick: () => actions.goTab('base') },
        el('div', { class: 'label', text: 'Build actions' }),
        el('div', { class: 'value', text: fmtInt(adm.totals.builds) }),
      ),
    );

    panel.append(
      card(
        'Activity (ADM)',
        p.hasTime ? null : el('p', { class: 'muted small', text: 'The log has no parseable timestamps, so the period is unknown.' }),
        kpis,
      ),
    );
  } else if (adm) {
    panel.append(card('Activity (ADM)', emptyState('No player events recognised in the ADM lines.')));
  } else {
    panel.append(card('Activity (ADM)', emptyState('No ADM loaded — drop an .ADM file to see players, kills and bases.')));
  }

  // Log state
  if (rpt) {
    const endText =
      rpt.endState === 'clean'
        ? 'Clean shutdown marker found at the end of the log.'
        : rpt.endState === 'crash'
          ? 'The log contains a crash marker — the process died instead of shutting down.'
          : 'No shutdown in the log (still running, killed or restarted hard).';

    const rows = [
      el('div', { class: 'small muted' }, el('strong', { text: 'End of log: ' }), endText),
      el('div', { class: 'small muted' }, el('strong', { text: 'Origin: ' }), originText(report)),
      el('div', { class: 'small muted' }, el('strong', { text: 'Mods: ' }), rpt.mods.length ? rpt.mods.join(', ') : 'none detected (vanilla)'),
    ];

    panel.append(card('Engine log', ...rows));
  }

  // Leaders
  if (adm && adm.players.length) {
    const leaders = adm.players.slice().sort((a, b) => b.kills - a.kills).slice(0, 8);
    const rows = leaders.map((p) =>
      el(
        'tr',
        {},
        el('td', {}, el('span', { style: `color:${colorFor(p.name)}`, text: p.name })),
        el('td', { class: 'num', text: fmtInt(p.kills) }),
        el('td', { class: 'num', text: fmtInt(p.deaths) }),
        el('td', { class: 'num', text: p.kd.toFixed(2) }),
        el('td', { class: 'num', text: fmtDuration(p.playtime) }),
      ),
    );

    const weapons = topWeapons(adm.kills, 8)
      .map(([w, n]) => el('tr', {}, el('td', { text: w }), el('td', { class: 'num', text: fmtInt(n) })))
      .slice(0, 8);

    panel.append(
      el(
        'div',
        { class: 'card' },
        el('h3', { text: 'Top players' }),
        table(
          [
            { label: 'Player' },
            { label: 'Kills', num: true },
            { label: 'Deaths', num: true },
            { label: 'K/D', num: true },
            { label: 'Playtime', num: true },
          ],
          rows,
        ),
      ),
      el(
        'div',
        { class: 'card' },
        el('h3', { text: 'Weapons used' }),
        weapons.length
          ? table([{ label: 'Weapon' }, { label: 'Kills', num: true }], weapons)
          : emptyState('No kills recorded.'),
      ),
    );
  }

  // Files
  const fileRows = files.map((f) =>
    el(
      'tr',
      {},
      el('td', {}, el('span', { class: `chip-type ${String(f.type).toLowerCase()}`, text: f.type })),
      el('td', { text: f.name }),
      el('td', { class: 'num', text: fmtInt(f.lines) }),
      el('td', { class: 'num', text: fmtBytes(f.bytes) }),
      el('td', { class: 'faint small', text: f.error ?? '' }),
    ),
  );

  panel.append(
    el(
      'div',
      { class: 'card' },
      el('h3', { text: 'Files' }),
      table(
        [{ label: 'Type' }, { label: 'Name' }, { label: 'Lines', num: true }, { label: 'Size', num: true }, { label: 'Note' }],
        fileRows,
      ),
    ),
  );
}

function originText(report) {
  if (!report.rpt) return '—';
  if (report.rptOrigin === 'server') return 'server RPT (dedicated server output)';
  if (report.rptOrigin === 'client') return 'game client log — this is your own session, not the server';
  return 'could not be determined';
}

/* ── Problems ────────────────────────────────────────────── */

export function renderProblems(panel, report, state) {
  clear(panel);

  if (!report.rpt) {
    panel.append(card('', emptyState('No RPT loaded. Drop an .RPT file to analyse engine messages.')));
    return;
  }

  const filter = state.problemFilter;
  const query = state.problemQuery.trim().toLowerCase();

  const chips = el(
    'div',
    { class: 'filters' },
    ...[['all', 'All'], ...SEVERITY_ORDER.map((s) => [s, SEVERITY_LABEL[s]])].map(([value, label]) =>
      el('button', {
        class: `filter${filter === value ? ' active' : ''}`,
        text: value === 'all' ? label : `${label} (${fmtInt(report.rpt.counts[value] ?? 0)})`,
        onClick: () => state.actions.setState({ problemFilter: value }),
      }),
    ),
    el('input', {
      type: 'search',
      placeholder: 'Filter messages …',
      value: state.problemQuery,
      class: 'filter',
      style: 'min-width:200px;border-radius:8px',
      'data-focus': 'problem-query',
      onInput: (e) => {
        state.problemQuery = e.target.value;
        state.actions.setState({ problemQuery: e.target.value });
      },
    }),
  );

  panel.append(chips);

  const groups = report.rpt.groups.filter((g) => {
    if (filter !== 'all' && g.severity !== filter) return false;
    if (!query) return true;
    return `${g.title} ${g.sample} ${g.category}`.toLowerCase().includes(query);
  });

  if (!groups.length) {
    panel.append(card('', emptyState('Nothing matches this filter.')));
    return;
  }

  if (report.rpt.overflowGroups > 0) {
    panel.append(
      el('p', {
        class: 'alert',
        text: `${fmtInt(report.rpt.overflowGroups)} additional rare patterns were collapsed to keep memory bounded.`,
      }),
    );
  }

  for (const g of groups.slice(0, 400)) {
    const timeRange =
      g.firstTod === null
        ? ''
        : g.firstTod === g.lastTod
          ? `at ${fmtTime(g.firstTod)}`
          : `${fmtTime(g.firstTod)} – ${fmtTime(g.lastTod)}`;

    panel.append(
      el(
        'div',
        { class: `problem ${g.severity}` },
        el(
          'div',
          { class: 'problem-head' },
          el(
            'div',
            {},
            el('div', { class: 'problem-title', text: g.title }),
            el('div', {
              class: 'small faint',
              text: [g.category, `${fmtInt(g.count)}×`, timeRange].filter(Boolean).join(' · '),
            }),
          ),
          el(
            'div',
            { style: 'display:flex;gap:6px;align-items:center' },
            g.vanilla ? el('span', { class: 'tag vanilla', text: 'also vanilla' }) : null,
            sevPill(g.severity),
          ),
        ),
        el(
          'div',
          { class: 'problem-body' },
          el('p', { text: g.explain }),
          g.fix ? el('p', { class: 'fix' }, el('strong', { text: 'Fix: ' }), g.fix) : null,
          g.samples.length
            ? el(
                'details',
                { class: 'samples' },
                el('summary', { text: `Show ${g.samples.length} example line${g.samples.length === 1 ? '' : 's'}` }),
                el('pre', { text: g.samples.join('\n') }),
              )
            : null,
        ),
      ),
    );
  }
}

/* ── Players ─────────────────────────────────────────────── */

export function renderPlayers(panel, report, state) {
  clear(panel);
  const adm = report.adm;

  if (!adm || !adm.players.length) {
    panel.append(card('', emptyState('No players recognised. Is this really an ADM file?')));
    return;
  }

  const query = state.playerQuery.trim().toLowerCase();
  const selected = state.selectedPlayers;
  const onlySelected = state.playersOnlySelected;
  const matches = adm.players.filter((p) => !query || p.name.toLowerCase().includes(query));
  const players = onlySelected ? matches.filter((p) => selected.has(p.key)) : matches;

  const toggle = (key, on) => {
    if (on) selected.add(key);
    else selected.delete(key);
    state.actions.setState({});
  };

  panel.append(
    el(
      'div',
      { class: 'filters' },
      el('input', {
        type: 'search',
        placeholder: 'Search players …',
        value: state.playerQuery,
        class: 'filter',
        style: 'min-width:220px;border-radius:8px',
        'data-focus': 'player-query',
        onInput: (e) => {
          state.playerQuery = e.target.value;
          state.actions.setState({ playerQuery: e.target.value });
        },
      }),
      el('button', {
        class: `filter${onlySelected ? ' active' : ''}`,
        text: onlySelected ? `Showing ${fmtInt(selected.size)} selected` : 'Show selected only',
        onClick: () => state.actions.setState({ playersOnlySelected: !onlySelected }),
      }),
      el('button', {
        class: 'filter',
        text: 'Select shown',
        title: 'Select every player matching the search',
        onClick: () => {
          for (const p of matches) selected.add(p.key);
          state.actions.setState({});
        },
      }),
      el('button', {
        class: 'filter',
        text: selected.size ? `Clear selection (${fmtInt(selected.size)})` : 'Clear selection',
        disabled: selected.size ? undefined : '',
        onClick: () => {
          selected.clear();
          state.actions.setState({});
        },
      }),
    ),
  );

  const rows = players.slice(0, 500).map((p) =>
    el(
      'tr',
      {
        class: selected.has(p.key) ? 'selected' : '',
        onClick: (event) => {
          // Clicking anywhere on the row toggles it, so several players can be
          // picked without reaching for the checkbox.
          if (event.target instanceof HTMLInputElement) return;
          toggle(p.key, !selected.has(p.key));
        },
        style: 'cursor:pointer',
      },
      el('td', {}, el('input', {
        type: 'checkbox',
        checked: selected.has(p.key) ? true : null,
        'aria-label': `Select ${p.name}`,
        onChange: (e) => toggle(p.key, e.target.checked),
      })),
      el('td', {}, el('span', { style: `color:${colorFor(p.name)}`, text: p.name })),
      el('td', { class: 'num', text: fmtInt(p.connects) }),
      el('td', { class: 'num', text: fmtDuration(p.playtime) }),
      el('td', { class: 'num', text: fmtInt(p.kills) }),
      el('td', { class: 'num', text: fmtInt(p.deaths) }),
      el('td', { class: 'num', text: p.kd.toFixed(2) }),
      el('td', { class: 'num', text: fmtInt(p.chats) }),
      el('td', { class: 'num', text: fmtInt(p.builds) }),
      el('td', { class: 'num', text: p.suspicion ? fmtInt(p.suspicion) : '' }),
    ),
  );

  panel.append(
    card(
      `Players (${fmtInt(players.length)})`,
      players.length
        ? table(
            [
              { label: '' },
              { label: 'Player' },
              { label: 'Sessions', num: true },
              { label: 'Playtime', num: true },
              { label: 'Kills', num: true },
              { label: 'Deaths', num: true },
              { label: 'K/D', num: true },
              { label: 'Chat', num: true },
              { label: 'Builds', num: true },
              { label: 'Flags', num: true },
            ],
            rows,
          )
        : emptyState(onlySelected ? 'No selected players match this search.' : 'No players match this search.'),
    ),
  );

  // Details for every selected player, newest selection last.
  for (const key of selected) {
    const p = adm.playerByKey.get(key);
    if (p) panel.append(playerDetail(report, p));
  }
}

function playerDetail(report, p) {
  const adm = report.adm;
  const involved = adm.kills.filter((k) => k.killerKey === p.key || k.victimKey === p.key);

  const sessions = p.sessions.slice(-25).map((s) =>
    el(
      'tr',
      {},
      el('td', { text: s.start === null ? '—' : fmtTime(s.start) }),
      el('td', { text: s.end === null ? 'still open' : fmtTime(s.end) }),
      el('td', { class: 'num', text: s.start !== null && s.end !== null ? fmtDuration(s.end - s.start) : '—' }),
      el('td', { class: 'num', text: fmtInt(s.kills) }),
      el('td', { class: 'num', text: fmtInt(s.deaths) }),
    ),
  );

  const killRows = p.weapons.map((use) =>
    el('tr', {}, el('td', { text: use.name }), el('td', { class: 'num', text: fmtInt(use.count) })),
  );

  const flags = adm.suspicious.filter((s) => s.playerKey === p.key).slice(0, 20).map((s) =>
    el(
      'tr',
      {},
      el('td', { text: s.at === null ? '—' : fmtTime(s.at) }),
      el('td', { text: s.type }),
      el('td', { text: s.detail }),
    ),
  );

  return el(
    'div',
    { class: 'card' },
    el('h3', { text: p.name }),
    el(
      'div',
      { class: 'kpis', style: 'margin-bottom:14px' },
      kpi('Playtime', fmtDuration(p.playtime), `${fmtInt(p.connects)} session(s)`),
      kpi('Kills', fmtInt(p.kills), `${fmtInt(p.hitsDealt)} hits landed`),
      kpi('Deaths', fmtInt(p.deaths), `${fmtInt(p.pvpDeaths)} PvP · ${fmtInt(p.aiDeaths)} other`),
      kpi('K/D', p.kd.toFixed(2), null),
      kpi('Position samples', fmtInt(p.positionCount), p.lastKnownPos ? `last <${p.lastKnownPos.x.toFixed(0)}, ${p.lastKnownPos.y.toFixed(0)}>` : null),
    ),
    el('h3', { text: 'Sessions' }),
    sessions.length
      ? table(
          [
            { label: 'Connected' },
            { label: 'Disconnected' },
            { label: 'Duration', num: true },
            { label: 'Kills', num: true },
            { label: 'Deaths', num: true },
          ],
          sessions,
        )
      : emptyState('No sessions recorded.'),
    el('h3', { text: 'Weapons' }),
    killRows.length ? table([{ label: 'Weapon' }, { label: 'Kills', num: true }], killRows) : emptyState('No kills.'),
    el('h3', { text: 'Engagements' }),
    involved.length
      ? table(
          [{ label: 'Time' }, { label: 'Killer' }, { label: 'Victim' }, { label: 'Weapon' }, { label: 'Distance', num: true }],
          involved.slice(0, 60).map((k) =>
            el(
              'tr',
              {},
              el('td', { text: k.at === null ? '—' : fmtTime(k.at) }),
              el('td', { text: k.killer }),
              el('td', { text: k.victim }),
              el('td', { text: k.weapon ?? '—' }),
              el('td', { class: 'num', text: k.distance === null ? '—' : fmtDistance(k.distance) }),
            ),
          ),
        )
      : emptyState('No kills or deaths.'),
    flags.length ? el('h3', { text: 'Flags' }) : null,
    flags.length ? table([{ label: 'Time' }, { label: 'Type' }, { label: 'Detail' }], flags) : null,
  );
}

/* ── Kills ───────────────────────────────────────────────── */

export function renderKills(panel, report, state) {
  clear(panel);
  const adm = report.adm;

  if (!adm || !adm.kills.length) {
    panel.append(card('', emptyState('No player-versus-player kills recorded.')));
    return;
  }

  const query = state.killQuery.trim().toLowerCase();
  let kills = adm.kills.filter(
    (k) => !query || `${k.killer} ${k.victim} ${k.weapon ?? ''}`.toLowerCase().includes(query),
  );

  if (state.killsSort === 'distance') {
    kills = kills.slice().sort((a, b) => (b.distance ?? -1) - (a.distance ?? -1));
  }

  panel.append(
    el(
      'div',
      { class: 'filters' },
      el('input', {
        type: 'search',
        placeholder: 'Search killer, victim or weapon …',
        value: state.killQuery,
        class: 'filter',
        style: 'min-width:240px;border-radius:8px',
        'data-focus': 'kill-query',
        onInput: (e) => {
          state.killQuery = e.target.value;
          state.actions.setState({ killQuery: e.target.value });
        },
      }),
      ...[
        ['time', 'Newest first'],
        ['distance', 'Longest shot'],
      ].map(([value, label]) =>
        el('button', {
          class: `filter${state.killsSort === value ? ' active' : ''}`,
          text: label,
          onClick: () => state.actions.setState({ killsSort: value }),
        }),
      ),
    ),
  );

  const ordered = state.killsSort === 'time' ? kills.slice().reverse() : kills;

  const rows = ordered.slice(0, 500).map((k) =>
    el(
      'tr',
      {},
      el('td', { text: k.at === null ? '—' : fmtTime(k.at) }),
      el('td', {}, el('span', { style: `color:${colorFor(k.killer)}`, text: k.killer })),
      el('td', {}, el('span', { style: `color:${colorFor(k.victim)}`, text: k.victim })),
      el('td', { text: k.weapon ?? '—' }),
      el('td', { class: 'num', text: k.distance === null ? '—' : fmtDistance(k.distance) }),
    ),
  );

  panel.append(
    card(
      `Kills (${fmtInt(kills.length)})`,
      table(
        [
          { label: 'Time' },
          { label: 'Killer' },
          { label: 'Victim' },
          { label: 'Weapon' },
          { label: 'Distance', num: true },
        ],
        rows,
      ),
      ordered.length > 500 ? el('p', { class: 'small faint', text: `Showing the first 500 of ${fmtInt(ordered.length)}.` }) : null,
    ),
  );
}

/* ── Base building ───────────────────────────────────────── */

export function renderBase(panel, report) {
  clear(panel);
  const adm = report.adm;

  if (!adm || !adm.builds.length) {
    panel.append(
      card(
        '',
        emptyState(
          'No build actions recorded. Note that DayZ only writes build lines to the ADM when the server has build logging enabled — a busy server with no build lines usually has logging off, not no bases.',
        ),
      ),
    );
    return;
  }

  const byObject = new Map();
  for (const b of adm.builds) {
    const key = (b.object || 'unknown').replace(/\s+(?:at|from)\s+<.*$/, '').trim().slice(0, 80);
    byObject.set(key, (byObject.get(key) ?? 0) + 1);
  }

  const objectRows = [...byObject.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, 20)
    .map(([name, n]) => el('tr', {}, el('td', { text: name }), el('td', { class: 'num', text: fmtInt(n) })));

  const rows = adm.builds
    .slice()
    .reverse()
    .slice(0, 400)
    .map((b) =>
      el(
        'tr',
        {},
        el('td', { text: b.at === null ? '—' : fmtTime(b.at) }),
        el('td', {}, el('span', { style: `color:${colorFor(b.player)}`, text: b.player })),
        el('td', { text: truncate(b.object, 90) }),
        el('td', {
          class: 'faint small',
          text: b.pos ? `<${b.pos.x.toFixed(0)}, ${b.pos.y.toFixed(0)}>` : '',
        }),
      ),
    );

  panel.append(
    card(
      `Build actions (${fmtInt(adm.builds.length)})`,
      table([{ label: 'Time' }, { label: 'Player' }, { label: 'Object' }, { label: 'Position' }], rows),
    ),
    card('Most built objects', table([{ label: 'Object' }, { label: 'Count', num: true }], objectRows)),
  );
}

/* ── Suspicious ──────────────────────────────────────────── */

const SUSPICIOUS_LABEL = {
  'combat-log': 'Combat logging',
  teleport: 'Impossible movement',
  'short-session': 'Very short session',
  'shared-id': 'Shared player id',
  kick: 'Kicked',
  ban: 'Banned',
};

export function renderSuspicious(panel, report, state) {
  clear(panel);
  const adm = report.adm;

  if (!adm || !adm.suspicious.length) {
    panel.append(
      card(
        '',
        emptyState(
          'Nothing flagged. Note that a flag is a hint, not proof: disconnects after a fight can be crashes, and position jumps can be server-side teleports by admins.',
        ),
      ),
    );
    return;
  }

  const types = [...new Set(adm.suspicious.map((s) => s.type))];
  const filter = state.suspiciousFilter;

  panel.append(
    el(
      'div',
      { class: 'filters' },
      el('button', {
        class: `filter${filter === 'all' ? ' active' : ''}`,
        text: `All (${fmtInt(adm.suspicious.length)})`,
        onClick: () => state.actions.setState({ suspiciousFilter: 'all' }),
      }),
      ...types.map((t) =>
        el('button', {
          class: `filter${filter === t ? ' active' : ''}`,
          text: `${SUSPICIOUS_LABEL[t] ?? t} (${fmtInt(adm.suspicious.filter((s) => s.type === t).length)})`,
          onClick: () => state.actions.setState({ suspiciousFilter: t }),
        }),
      ),
    ),
  );

  const list = adm.suspicious.filter((s) => filter === 'all' || s.type === filter);

  panel.append(
    card(
      'Flagged events',
      table(
        [{ label: 'Time' }, { label: 'Player' }, { label: 'Type' }, { label: 'Detail' }],
        list.slice(0, 500).map((s) =>
          el(
            'tr',
            {},
            el('td', { text: s.at === null ? '—' : fmtTime(s.at) }),
            el('td', {}, el('span', { style: `color:${colorFor(s.player)}`, text: s.player })),
            el('td', { text: SUSPICIOUS_LABEL[s.type] ?? s.type }),
            el('td', { class: 'small muted', text: s.detail }),
          ),
        ),
      ),
    ),
  );

  const scored = adm.players.filter((p) => p.suspicion > 0).sort((a, b) => b.suspicion - a.suspicion).slice(0, 15);
  panel.append(
    card(
      'Players to review',
      scored.length
        ? table(
            [{ label: 'Player' }, { label: 'Weight', num: true }, { label: 'Flags', num: true }],
            scored.map((p) =>
              el(
                'tr',
                {},
                el('td', {}, el('span', { style: `color:${colorFor(p.name)}`, text: p.name })),
                el('td', { class: 'num', text: fmtInt(p.suspicion) }),
                el('td', { class: 'num', text: fmtInt(adm.suspicious.filter((s) => s.playerKey === p.key).length) }),
              ),
            ),
          )
        : emptyState('No players scored.'),
      el('p', {
        class: 'small faint',
        text: 'Weights: combat log +2, impossible movement +3, shared id +1, very short session +1. Use them to prioritise, never as evidence.',
      }),
    ),
  );
}

/* ── Timeline ────────────────────────────────────────────── */

/** "HH:MM" → seconds since midnight, or null when the field is empty. */
function parseClock(value) {
  const m = /^(\d{1,2}):(\d{2})$/.exec(String(value ?? '').trim());
  if (!m) return null;
  const hours = Number(m[1]);
  const minutes = Number(m[2]);
  if (hours > 23 || minutes > 59) return null;
  return hours * 3600 + minutes * 60;
}

function fmtClockInput(secs) {
  const s = Math.max(0, Math.min(86340, secs));
  return `${String(Math.floor(s / 3600)).padStart(2, '0')}:${String(Math.floor((s % 3600) / 60)).padStart(2, '0')}`;
}

export function renderTimeline(panel, report, state) {
  clear(panel);
  const adm = report.adm;

  if (!adm || !adm.events) {
    panel.append(card('', emptyState('No timestamped ADM events.')));
    return;
  }

  // Narrow the window: a night shift can be looked at without the whole day.
  const from = parseClock(state.timelineFrom);
  const to = parseClock(state.timelineTo);
  const wrapped = from !== null && to !== null && from > to;
  const inWindow = (tod) => {
    if (!Number.isFinite(tod)) return false;
    if (from === null && to === null) return true;
    const start = from ?? 0;
    const end = to ?? 86399;
    if (start === end) return true;
    // An overturned range (22:00 → 04:00) is a night window across midnight.
    return wrapped ? tod >= start || tod <= end : tod >= start && tod <= end;
  };

  const buckets = Array.from({ length: 24 }, () => ({ connects: 0, kills: 0, chats: 0, builds: 0 }));
  // ADM events carry absolute epoch seconds; the timeline works in the log's
  // own clock, so reduce to seconds-since-midnight first.
  const todOf = (t) => {
    if (!Number.isFinite(t)) return NaN;
    const r = t % 86400;
    return r < 0 ? r + 86400 : r;
  };
  const bucketOf = (tod) => Math.max(0, Math.min(23, Math.floor(tod / 3600)));
  const bump = (at, field) => {
    const tod = todOf(at);
    if (!inWindow(tod)) return;
    buckets[bucketOf(tod)][field]++;
  };

  const total = { connects: 0, kills: 0, chats: 0, builds: 0 };
  const count = (at, field) => {
    if (!inWindow(todOf(at))) return;
    total[field]++;
  };

  for (const p of adm.players) {
    for (const s of p.sessions) {
      bump(s.start, 'connects');
      count(s.start, 'connects');
    }
  }
  for (const k of adm.kills) {
    bump(k.at, 'kills');
    count(k.at, 'kills');
  }
  for (const c of adm.chats) {
    bump(c.at, 'chats');
    count(c.at, 'chats');
  }
  for (const b of adm.builds) {
    bump(b.at, 'builds');
    count(b.at, 'builds');
  }

  const windowLabel = from === null && to === null
    ? 'the whole log'
    : `${fmtClockInput(from ?? 0)} – ${fmtClockInput(to ?? 86399)}${wrapped ? ' (next day)' : ''}`;

  const rangeControls = el(
    'div',
    { class: 'filters', style: 'align-items:center' },
    el('span', { class: 'small muted', text: 'From' }),
    el('input', {
      type: 'time',
      value: state.timelineFrom,
      class: 'filter',
      style: 'border-radius:8px',
      'aria-label': 'Start of the time window',
      onChange: (e) => {
        state.timelineFrom = e.target.value;
        state.actions.setState({ timelineFrom: e.target.value });
      },
    }),
    el('span', { class: 'small muted', text: 'to' }),
    el('input', {
      type: 'time',
      value: state.timelineTo,
      class: 'filter',
      style: 'border-radius:8px',
      'aria-label': 'End of the time window',
      onChange: (e) => {
        state.timelineTo = e.target.value;
        state.actions.setState({ timelineTo: e.target.value });
      },
    }),
    el('button', {
      class: 'filter',
      text: 'Whole day',
      onClick: () => {
        state.timelineFrom = '';
        state.timelineTo = '';
        state.actions.setState({ timelineFrom: '', timelineTo: '' });
      },
    }),
    ...[['18:00', '23:59', 'Evening'], ['00:00', '06:00', 'Night']].map(([f, t, label]) =>
      el('button', {
        class: 'filter',
        text: label,
        onClick: () => {
          state.timelineFrom = f;
          state.timelineTo = t;
          state.actions.setState({ timelineFrom: f, timelineTo: t });
        },
      }),
    ),
    el('span', { class: 'small faint', text: `Showing ${windowLabel}` }),
  );

  const peak = Math.max(1, ...buckets.map((b) => Math.max(b.connects, b.kills, b.chats, b.builds)));

  const chart = el(
    'div',
    {
      style: 'display:flex;align-items:flex-end;gap:4px;height:190px;padding:10px 4px;border:1px solid var(--border-soft);border-radius:8px;background:#0d1116;overflow-x:auto',
    },
    ...buckets.map((b, hour) =>
      el(
        'div',
        {
          style: `flex:1 0 22px;display:flex;flex-direction:column;justify-content:flex-end;height:100%;gap:2px;${inWindow(hour * 3600 + 1) ? '' : 'opacity:0.28'}`,
          title: `${String(hour).padStart(2, '0')}:00 — ${b.connects} joins, ${b.kills} kills, ${b.chats} chat, ${b.builds} builds`,
        },
        el('div', { style: `height:${(b.kills / peak) * 100}%;background:var(--critical);border-radius:2px 2px 0 0;min-height:${b.kills ? 2 : 0}px` }),
        el('div', { style: `height:${(b.chats / peak) * 100}%;background:var(--info);min-height:${b.chats ? 2 : 0}px` }),
        el('div', { style: `height:${(b.connects / peak) * 100}%;background:var(--ok);min-height:${b.connects ? 2 : 0}px` }),
        el('div', { style: `height:${(b.builds / peak) * 100}%;background:#5ac8dc;min-height:${b.builds ? 2 : 0}px` }),
        el('div', { style: 'font-size:9.5px;color:var(--text-faint);text-align:center;padding-top:2px', text: String(hour).padStart(2, '0') }),
      ),
    ),
  );

  panel.append(
    card(
      'Activity by hour',
      rangeControls,
      chart,
      el(
        'div',
        { class: 'legend' },
        el('span', {}, el('i', { style: 'background:var(--critical)' }), 'Kills'),
        el('span', {}, el('i', { style: 'background:var(--info)' }), 'Chat'),
        el('span', {}, el('i', { style: 'background:var(--ok)' }), 'Joins'),
        el('span', {}, el('i', { style: 'background:#5ac8dc' }), 'Builds'),
      ),
      el('p', {
        class: 'small faint',
        text: `${fmtInt(total.connects)} joins, ${fmtInt(total.kills)} kills, ${fmtInt(total.chats)} chat messages and ${fmtInt(total.builds)} build actions fall inside this window.`,
      }),
    ),
  );

  const hottest = buckets
    .map((b, hour) => ({ hour, total: b.kills * 3 + b.connects * 2 + b.chats + b.builds }))
    .sort((a, b) => b.total - a.total)
    .slice(0, 5)
    .filter((b) => b.total > 0);

  if (hottest.length) {
    panel.append(
      card(
        'Busiest hours',
        table(
          [{ label: 'Hour' }, { label: 'Joins', num: true }, { label: 'Kills', num: true }, { label: 'Chat', num: true }, { label: 'Builds', num: true }],
          hottest.map(({ hour }) =>
            el(
              'tr',
              {},
              el('td', { text: `${String(hour).padStart(2, '0')}:00` }),
              el('td', { class: 'num', text: fmtInt(buckets[hour].connects) }),
              el('td', { class: 'num', text: fmtInt(buckets[hour].kills) }),
              el('td', { class: 'num', text: fmtInt(buckets[hour].chats) }),
              el('td', { class: 'num', text: fmtInt(buckets[hour].builds) }),
            ),
          ),
        ),
      ),
    );
  }
}

/* ── Map ─────────────────────────────────────────────────── */

// Hint under the map: explains what is drawn and how to change it.
function mapHint(mapMeta, mapState, state) {
  const coords = 'Coordinates are DayZ world metres: x = east, y = north, z = altitude.';
  if (mapState.background) {
    const src = mapState.backgroundSource === 'server'
      ? 'served by dayzctl for this instance'
      : mapState.backgroundSource || 'loaded image';
    return `Background: ${src}. ${coords}`;
  }
  if (mapMeta?.tileUrl) {
    const label = mapMeta.name || mapMeta.id;
    return `Bundled imagery and place names for ${label} (${fmtInt(mapMeta.size)} m). ${coords}`;
  }
  if (state.mapId === '__none__') {
    return `No imagery selected \u2014 choose a map above to draw the terrain. ${coords}`;
  }
  if (state.mapMetaFailed) {
    return `No bundled imagery for \u201c${state.mapId || 'this instance'}\u201d. Pick another map above, or set instances[].map in the configuration. ${coords}`;
  }
  return `Loading map imagery\u2026 Put map.jpg in the instance\u2019s profiles folder, or set instances[].map_image, to use your own. ${coords}`;
}

export function renderMapPanel(panel, report, state, relayout) {
  clear(panel);
  const adm = report.adm;

  if (!adm || !adm.players.length) {
    panel.append(card('', emptyState('No ADM positions to plot.')));
    return;
  }

  // The map's own extent wins; without one, scale to what the log contains.
  const autoWorld = inferWorldSize(adm.bounds);
  const mapState = state.map;
  const mapMeta = state.mapMeta;
  const metaWorld = mapMeta?.size ? mapMeta.size : 0;
  const effectiveWorld = mapState.worldSize || metaWorld || autoWorld;
  const tiles = mapMeta?.tileUrl
    ? {
        url: mapMeta.tileUrl,
        minZoom: mapMeta.minZoom || 1,
        maxZoom: mapMeta.maxZoom || 7,
        names: mapMeta.names || [],
      }
    : null;
  const view = { ...mapState, worldSize: effectiveWorld, tiles };
  // Players ticked in the Players tab, or in the picker below, are drawn on
  // their own; with nothing ticked the busiest trails are shown.
  if (state.selectedPlayers.size) view.selectedKeys = [...state.selectedPlayers];
  // The resize handler repaints with this exact view (world size and tiles are
  // resolved here, not in the raw state).
  state.lastMapView = view;

  // Map selector: the instance's own map, any other installed map, or no
  // imagery at all. Switching reloads that map's extent and place names.
  const pickMap = (id) => {
    state.mapId = id;
    state.mapMeta = null;
    state.mapMetaFailed = false;
    state.mapMetaLoading = false;
    mapState.worldSize = 0;
    // A different map means a different extent, so start from the whole view.
    mapState.zoom = 1;
    mapState.centerX = null;
    mapState.centerY = null;
    state.actions.setState({});
  };

  const mapOptions = [
    el('option', {
      value: '',
      text: state.instanceMapId
        ? `Instance map (${state.instanceMapId})`
        : 'Instance map (none configured)',
      selected: state.mapId === state.instanceMapId || !state.mapId,
    }),
    ...state.availableMaps
      .filter((m) => m.id !== state.instanceMapId)
      .map((m) =>
        el('option', {
          value: m.id,
          text: `${m.name || m.id} (${fmtInt(m.size)} m)`,
          selected: state.mapId === m.id,
        }),
      ),
    el('option', {
      value: '__none__',
      text: 'No imagery (grid)',
      selected: state.mapId === '__none__',
    }),
  ];

  const mapSelect = el(
    'select',
    {
      onChange: (e) => {
        const value = e.target.value;
        if (value === '__none__') pickMap('__none__');
        else pickMap(value || state.instanceMapId);
      },
    },
    ...mapOptions,
  );

  const worldInput = el('input', {
    type: 'number',
    min: '1000',
    step: '100',
    value: String(effectiveWorld),
    onChange: (e) => {
      mapState.worldSize = Math.max(1000, Number(e.target.value) || 15360);
      view.worldSize = mapState.worldSize;
      // A new extent invalidates the old centre; keep the zoom, recentre the map.
      mapState.centerX = null;
      mapState.centerY = null;
      // Repaint in place so an open player picker is not torn down.
      if (canvas.isConnected) renderMap(canvas, report, view);
    },
  });

  // Map imagery comes from the map service for this instance's map (Nasdara,
  // Chernarus, …). A file picked here simply overrides it for this session.
  const imageUrl = state.mapImageUrl || '';
  if (imageUrl && !mapState.background && !mapState.imageFailed && mapState.imageUrl !== imageUrl) {
    mapState.imageUrl = imageUrl;
    const img = new Image();
    img.onload = () => {
      mapState.background = img;
      mapState.backgroundSource = 'server';
      state.actions.setState({});
    };
    img.onerror = () => {
      mapState.imageFailed = true;
    };
    img.src = imageUrl;
  }

  // Fetch the selected map's extent and place names.
  if (state.mapId && state.mapId !== '__none__' && !state.mapMeta && !state.mapMetaFailed && !state.mapMetaLoading) {
    state.mapMetaLoading = true;
    fetchMapMeta(state.mapId)
      .then((meta) => {
        state.mapMeta = meta;
        state.mapMetaLoading = false;
        state.actions.setState({});
      })
      .catch(() => {
        state.mapMetaLoading = false;
        state.mapMetaFailed = true;
      });
  }

  const bgInput = el('input', {
    type: 'file',
    accept: 'image/*',
    onChange: (e) => {
      const file = e.target.files?.[0];
      if (!file) return;
      const img = new Image();
      img.onload = () => {
        mapState.background = img;
        mapState.backgroundSource = file.name;
        mapState.imageFailed = false;
        state.actions.setState({});
      };
      img.src = URL.createObjectURL(file);
    },
  });

  const canvas = el('canvas', { id: 'map-canvas' });
  const stage = el('div', { class: 'map-stage' }, canvas);

  // ── zoom & pan ───────────────────────────────────────────
  // Wheel zooms about the pointer, dragging pans, double-click fits the map
  // again. clampView keeps the zoom at >= 1 (the whole map) and stops panning
  // from revealing empty space around it.
  const zoomLabel = el('span', { class: 'map-zoom-value', text: '1.0×' });

  const syncZoomLabel = () => {
    zoomLabel.textContent = `${(mapState.zoom || 1).toFixed(1)}×`;
  };
  syncZoomLabel();

  // `view` is the snapshot the renderer draws, so zoom and pan have to be copied
  // into it as well, not only into mapState.
  const applyViewport = () => {
    view.zoom = mapState.zoom;
    view.centerX = mapState.centerX;
    view.centerY = mapState.centerY;
    repaintMap();
  };

  // Zooming in past the deepest tile level just stretches pixels, so the
  // ceiling follows the map's own pyramid (about 4x upscaling at most).
  const zoomCeiling = () => {
    const vp = view.viewport;
    const tileMax = state.mapMeta?.maxZoom;
    if (!vp) return MAX_MAP_ZOOM;
    if (!tileMax) return 16; // no imagery: the grid has nothing to resolve
    const limit = (4 * 2 ** tileMax * 256) / Math.max(1, vp.width);
    return Math.max(2, Math.min(MAX_MAP_ZOOM, limit));
  };

  const zoomTo = (nextZoom, anchorX = null, anchorY = null) => {
    const vp = view.viewport;
    if (!vp) return;
    const zoom = Math.min(zoomCeiling(), Math.max(1, nextZoom));
    const ax = anchorX ?? vp.width / 2;
    const ay = anchorY ?? vp.height / 2;
    // World point currently under the anchor stays put while scaling.
    const worldX = vp.cx + (ax - vp.width / 2) / vp.k;
    const worldY = vp.cy - (ay - vp.height / 2) / vp.k;
    const kNext = (vp.width / vp.world) * zoom;
    const clamped = clampView(
      vp.world,
      zoom,
      vp.width,
      worldX - (ax - vp.width / 2) / kNext,
      worldY + (ay - vp.height / 2) / kNext,
    );
    mapState.zoom = clamped.zoom;
    mapState.centerX = clamped.cx;
    mapState.centerY = clamped.cy;
    syncZoomLabel();
    applyViewport();
  };

  let drag = null;
  const endDrag = () => {
    if (!drag) return;
    drag = null;
    canvas.classList.remove('dragging');
  };

  canvas.addEventListener('pointerdown', (event) => {
    if (event.button !== 0 || !view.viewport) return;
    drag = { x: event.clientX, y: event.clientY, cx: view.viewport.cx, cy: view.viewport.cy };
    canvas.setPointerCapture(event.pointerId);
    canvas.classList.add('dragging');
  });

  canvas.addEventListener('pointermove', (event) => {
    if (!drag || !view.viewport) return;
    const vp = view.viewport;
    const moved = clampView(
      vp.world,
      vp.zoom,
      vp.width,
      drag.cx - (event.clientX - drag.x) / vp.k,
      drag.cy + (event.clientY - drag.y) / vp.k,
    );
    mapState.centerX = moved.cx;
    mapState.centerY = moved.cy;
    applyViewport();
  });

  canvas.addEventListener('pointerup', endDrag);
  canvas.addEventListener('pointercancel', endDrag);
  canvas.addEventListener('pointerleave', endDrag);

  canvas.addEventListener(
    'wheel',
    (event) => {
      if (!view.viewport) return;
      event.preventDefault();
      const rect = canvas.getBoundingClientRect();
      const factor = Math.exp(-event.deltaY * 0.0015);
      zoomTo((mapState.zoom || 1) * factor, event.clientX - rect.left, event.clientY - rect.top);
    },
    { passive: false },
  );

  canvas.addEventListener('dblclick', () => zoomTo(1));

  // Player picker: any combination of players can be ticked, instead of
  // all-or-one. The set is shared with the Players tab, so ticks made there
  // show up here and vice versa. Ticking repaints the canvas directly rather
  // than re-rendering the panel, so the popover stays open while picking.
  const repaintMap = () => {
    if (canvas.isConnected) renderMap(canvas, report, view);
  };
  const chosen = state.selectedPlayers;
  const summaryText = () =>
    chosen.size
      ? `${fmtInt(chosen.size)} of ${fmtInt(adm.players.length)} selected`
      : 'All players (top 12 trails)';
  const pickerSummary = el('span', { text: summaryText() });

  const syncSelection = () => {
    pickerSummary.textContent = summaryText();
    view.selectedKeys = chosen.size ? [...chosen] : undefined;
    selectShownBtn.textContent = shownAll() ? 'Unselect shown' : 'Select shown';
    clearBtn.disabled = chosen.size ? false : true;
    repaintMap();
  };

  const pickerList = el('div', { class: 'picker-list' });
  const pickerSearch = el('input', {
    type: 'search',
    placeholder: 'Filter players …',
    class: 'picker-search',
    'aria-label': 'Filter the player list',
    onInput: () => fillPicker(),
  });

  // Rows matching the current filter; the bulk buttons below act on these.
  let shown = [];
  const shownAll = () => shown.length > 0 && shown.every((p) => chosen.has(p.key));

  const selectShownBtn = el('button', {
    type: 'button',
    class: 'filter',
    text: 'Select shown',
    title: 'Tick or untick every player matching the filter',
    onClick: () => {
      const on = shownAll();
      for (const p of shown) {
        if (on) chosen.delete(p.key);
        else chosen.add(p.key);
      }
      fillPicker();
      syncSelection();
    },
  });

  const clearBtn = el('button', {
    type: 'button',
    class: 'filter',
    text: 'Clear',
    title: 'Go back to the busiest trails',
    onClick: () => {
      chosen.clear();
      fillPicker();
      syncSelection();
    },
  });
  clearBtn.disabled = !chosen.size;

  function fillPicker() {
    const query = pickerSearch.value.trim().toLowerCase();
    const matches = adm.players.filter((p) => !query || p.name.toLowerCase().includes(query));
    shown = matches.slice(0, 400);

    const rows = shown.map((p) =>
      el(
        'label',
        { class: 'picker-item', title: `${fmtInt(p.connects)} session(s) · ${fmtDuration(p.playtime)}` },
        el('input', {
          type: 'checkbox',
          checked: chosen.has(p.key) ? true : null,
          onChange: (e) => {
            if (e.target.checked) chosen.add(p.key);
            else chosen.delete(p.key);
            syncSelection();
          },
        }),
        el('span', { style: `color:${colorFor(p.name)}`, text: p.name }),
      ),
    );

    selectShownBtn.textContent = shownAll() ? 'Unselect shown' : 'Select shown';
    clearBtn.disabled = !chosen.size;

    pickerList.replaceChildren(
      el('div', { class: 'picker-actions' }, selectShownBtn, clearBtn),
      ...(rows.length ? rows : [el('p', { class: 'small faint', text: 'No player matches this filter.' })]),
    );
  }
  fillPicker();

  const playerSelect = el(
    'details',
    { class: 'picker' },
    el('summary', { title: 'Choose any number of players to draw' }, pickerSummary),
    el('div', { class: 'picker-panel' }, pickerSearch, pickerList),
  );

  // Timeline window: two thumbs over the analysed period. Everything the map
  // draws (trails, heat, kills, bases, jumps, last positions) is limited to the
  // selected span, so a busy day can be narrowed to one fight.
  const windowControl = (() => {
    const period = adm.period || {};
    const canWindow =
      period.hasTime && Number.isFinite(period.from) && Number.isFinite(period.to) && period.to - period.from >= 120;

    if (!canWindow) {
      return el(
        'div',
        { class: 'map-window' },
        el('span', { class: 'map-window-label', text: 'Timeline' }),
        el('span', { class: 'small faint', text: 'no usable timestamps in the log, so the whole file is drawn' }),
      );
    }

    const lo = period.from;
    const hi = period.to;
    if (!Number.isFinite(mapState.windowFrom) || mapState.windowFrom < lo || mapState.windowFrom > hi) {
      mapState.windowFrom = lo;
    }
    if (!Number.isFinite(mapState.windowTo) || mapState.windowTo < lo || mapState.windowTo > hi) {
      mapState.windowTo = hi;
    }
    if (mapState.windowFrom > mapState.windowTo) mapState.windowFrom = mapState.windowTo;

    // ~720 slider positions regardless of how long the log runs.
    const step = Math.max(1, Math.round((hi - lo) / 720));
    const multiDay = hi - lo > 86400;
    const stamp = (t) => {
      const d = new Date(Math.round(t) * 1000);
      const pad = (n) => String(n).padStart(2, '0');
      const clock = `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
      return multiDay ? `${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${clock}` : clock;
    };

    const fill = el('div', { class: 'dual-fill' });
    const valueText = el('span', { class: 'map-window-value' });
    const pct = (t) => ((t - lo) / (hi - lo || 1)) * 100;

    const paint = () => {
      const a = pct(mapState.windowFrom);
      const b = pct(mapState.windowTo);
      fill.style.left = `${a}%`;
      fill.style.width = `${Math.max(0, b - a)}%`;
      fromInput.value = String(mapState.windowFrom);
      toInput.value = String(mapState.windowTo);
      valueText.textContent = `${stamp(mapState.windowFrom)} → ${stamp(mapState.windowTo)} (${fmtDuration(
        mapState.windowTo - mapState.windowFrom,
      )})`;
      // The canvas repaints with this exact view on resize.
      view.windowFrom = mapState.windowFrom;
      view.windowTo = mapState.windowTo;
    };

    const fromInput = el('input', {
      type: 'range',
      class: 'dual-input',
      min: String(lo),
      max: String(hi),
      step: String(step),
      'aria-label': 'Timeline from',
      onInput: () => {
        mapState.windowFrom = Math.min(Number(fromInput.value), mapState.windowTo);
        paint();
        repaintMap();
      },
    });
    const toInput = el('input', {
      type: 'range',
      class: 'dual-input',
      min: String(lo),
      max: String(hi),
      step: String(step),
      'aria-label': 'Timeline until',
      onInput: () => {
        mapState.windowTo = Math.max(Number(toInput.value), mapState.windowFrom);
        paint();
        repaintMap();
      },
    });

    paint();

    return el(
      'div',
      { class: 'map-window' },
      el('span', { class: 'map-window-label', text: 'Timeline' }),
      el(
        'div',
        { class: 'dual-range' },
        el('div', { class: 'dual-track' }, fill),
        fromInput,
        toInput,
      ),
      valueText,
      el('button', {
        type: 'button',
        class: 'filter',
        text: 'Whole period',
        title: 'Reset the window to the whole analysed period',
        onClick: () => {
          mapState.windowFrom = lo;
          mapState.windowTo = hi;
          paint();
          repaintMap();
        },
      }),
    );
  })();

  const layerToggle = (key, label) =>
    el(
      'label',
      { class: 'filter', style: 'display:inline-flex;gap:6px;align-items:center;cursor:pointer' },
      el('input', {
        type: 'checkbox',
        checked: mapState.layers[key],
        onChange: (e) => {
          mapState.layers[key] = e.target.checked;
          repaintMap();
        },
      }),
      label,
    );

  // Tiles arrive asynchronously; coalesce repaints so a burst of loads does not
  // redraw the canvas dozens of times.
  let repaintPending = false;
  view.onTile = () => {
    if (repaintPending) return;
    repaintPending = true;
    setTimeout(() => {
      repaintPending = false;
      if (canvas.isConnected) renderMap(canvas, report, view);
    }, 40);
  };

  panel.append(
    card(
      'ADM on the map',
      el(
        'div',
        { class: 'map-controls' },
        el('div', { class: 'field' }, el('label', { text: 'Map' }), mapSelect),
        el('div', { class: 'field' }, el('label', { text: 'World size (m)' }), worldInput),
        el('div', { class: 'field' }, el('label', { text: 'Players' }), playerSelect),
        el('div', { class: 'field' }, el('label', { text: 'Custom image' }), bgInput),
        el(
          'div',
          { class: 'field' },
          el('label', { text: 'Zoom' }),
          el(
            'div',
            { class: 'map-zoom' },
            el('button', {
              type: 'button',
              class: 'btn tiny',
              text: '−',
              title: 'Zoom out (wheel down)',
              'aria-label': 'Zoom out',
              onClick: () => zoomTo((mapState.zoom || 1) / 1.5),
            }),
            zoomLabel,
            el('button', {
              type: 'button',
              class: 'btn tiny',
              text: '+',
              title: 'Zoom in (wheel up)',
              'aria-label': 'Zoom in',
              onClick: () => zoomTo((mapState.zoom || 1) * 1.5),
            }),
            el('button', {
              type: 'button',
              class: 'btn tiny',
              text: 'Fit',
              title: 'Show the whole map (double-click)',
              onClick: () => zoomTo(1),
            }),
          ),
        ),
      ),
      windowControl,
      el(
        'div',
        { class: 'filters' },
        layerToggle('tiles', 'Map imagery'),
        layerToggle('names', 'Place names'),
        layerToggle('kills', 'Kills'),
        layerToggle('trails', 'Trails'),
        layerToggle('builds', 'Bases'),
        layerToggle('heat', 'Heatmap'),
        layerToggle('connects', 'Last positions'),
        layerToggle('jumps', 'Position jumps'),
      ),
      stage,
      el(
        'div',
        { class: 'legend' },
        el('span', {}, el('i', { style: 'background:#e5484d' }), 'Kill position'),
        el('span', {}, el('i', { style: 'background:#5ac8dc' }), 'Build action'),
        el('span', {}, el('i', { style: 'background:var(--warning)' }), 'Impossible movement'),
      ),
      el('p', { class: 'small faint', text: mapHint(mapMeta, mapState, state) }),
    ),
  );

  // The canvas is already in the document, so it can be painted immediately.
  // Do not rely on requestAnimationFrame: browsers pause it in background tabs,
  // which would leave the map blank until the next resize.
  renderMap(canvas, report, view);
  relayout?.();
  setTimeout(() => {
    if (canvas.isConnected) renderMap(canvas, report, view);
  }, 60);
}

export { SEVERITY };
