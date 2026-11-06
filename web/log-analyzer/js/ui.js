/**
 * View layer. Pure DOM building — no framework, no innerHTML for log content
 * (log lines are untrusted text and are always set via textContent).
 */

import { fmtInt, fmtDuration, fmtTime, fmtClock, fmtDistance, truncate, colorFor } from './format.js';
import { renderMap, inferWorldSize, clampView, MAX_MAP_ZOOM } from './mapview.js';
import { fetchMapMeta, fetchBans, playerAction } from './api.js';

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

/* ── Overview ────────────────────────────────────────────── */

export function renderOverview(panel, report, actions) {
  clear(panel);
  const { adm } = report;

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
    panel.append(card('Activity (ADM)', emptyState('No ADM log in this profiles folder, so there are no players, kills or bases to show.')));
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
}

/* ── Player actions (RCON) ───────────────────────────────── */

// A player GUID is either the 64-bit SteamID that `players` reports or the
// 32-hex BattlEye GUID that the ban list prints.
const PLAYER_GUID = /^(7656119\d{10}|[0-9a-f]{32})$/i;

/** Small transient message in the bottom-right corner. */
function notify(message, tone = 'info') {
  let stack = document.querySelector('.toast-stack');
  if (!stack) {
    stack = el('div', { class: 'toast-stack', role: 'status', 'aria-live': 'polite' });
    document.body.append(stack);
  }
  const toast = el('div', { class: `toast ${tone}`, onClick: () => toast.remove() }, el('span', { text: message }));
  stack.append(toast);
  setTimeout(() => toast.remove(), 7000);
}

/**
 * Modal confirmation. Resolves to {ok, values} once dismissed; `values` holds
 * the optional form fields. Every destructive RCON action goes through here.
 */
function actionDialog({ title, description, confirmLabel = 'Confirm', danger = true, fields = [] }) {
  return new Promise((resolve) => {
    const inputs = new Map();
    const fieldNodes = fields.map((field) => {
      const control = field.options
        ? el(
            'select',
            { class: 'modal-input', name: field.name },
            field.options.map((opt) =>
              el('option', {
                value: opt.value,
                text: opt.label,
                selected: String(opt.value) === String(field.value) ? true : null,
              }),
            ),
          )
        : el('input', {
            class: 'modal-input',
            type: 'text',
            name: field.name,
            placeholder: field.placeholder ?? '',
            value: field.value ?? '',
            autocomplete: 'off',
          });
      inputs.set(field.name, control);
      return el('label', { class: 'modal-field' }, el('span', { class: 'modal-label', text: field.label }), control);
    });

    let settled = false;
    const finish = (ok) => {
      if (settled) return;
      settled = true;
      const values = Object.fromEntries([...inputs].map(([name, node]) => [name, node.value]));
      resolve({ ok, values });
      if (dialog.open) dialog.close();
      dialog.remove();
    };

    const dialog = el(
      'dialog',
      {
        class: 'modal',
        onCancel: (event) => {
          event.preventDefault();
          finish(false);
        },
        onClose: () => finish(false),
      },
      el('h3', { class: 'modal-title', text: title }),
      el('p', { class: 'modal-text', text: description }),
      ...fieldNodes,
      el(
        'div',
        { class: 'modal-actions' },
        el('button', { type: 'button', class: 'btn', text: 'Cancel', onClick: () => finish(false) }),
        el('button', {
          type: 'button',
          class: `btn${danger ? ' danger' : ' primary'}`,
          text: confirmLabel,
          onClick: () => finish(true),
        }),
      ),
    );

    // Enter confirms from a field; a focused button still activates normally.
    dialog.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' && event.target.tagName !== 'BUTTON') {
        event.preventDefault();
        finish(true);
      }
    });

    document.body.append(dialog);
    dialog.showModal();
    (dialog.querySelector('.btn.primary, .btn.danger') ?? dialog).focus();
  });
}

/** Confirmation copy, and optional inputs, for one action. */
function actionSpec(action, player) {
  if (action === 'kick') {
    return {
      title: `Kick ${player.name}?`,
      description:
        'Disconnects the player now. The RCON session list is used to find them, so this only works while they are connected.',
      confirmLabel: 'Kick',
      fields: [{ name: 'reason', label: 'Reason (optional)', placeholder: 'e.g. behaviour' }],
    };
  }
  if (action === 'ban') {
    return {
      title: `Ban ${player.name}?`,
      description: 'Bans the player and disconnects them immediately. The entry is written to the server ban list.',
      confirmLabel: 'Ban',
      fields: [
        {
          name: 'minutes',
          label: 'Duration',
          value: '0',
          options: [
            { value: '0', label: 'Permanent' },
            { value: '60', label: '1 hour' },
            { value: '1440', label: '1 day' },
            { value: '10080', label: '7 days' },
            { value: '43200', label: '30 days' },
          ],
        },
        { name: 'reason', label: 'Reason (optional)', placeholder: 'e.g. cheating' },
      ],
    };
  }
  return {
    title: `Unban ${player.name}?`,
    description:
      "Removes the active ban that matches this player's GUID. The GUID comes from the analysed ADM log, so this works even while the player is offline.",
    confirmLabel: 'Unban',
    danger: false,
    fields: [],
  };
}

async function runPlayerAction(state, player, action) {
  if (!state.instance) return;

  const { ok, values } = await actionDialog(actionSpec(action, player));
  if (!ok) return;

  const payload = { name: player.name, ids: (player.ids ?? []).filter((id) => PLAYER_GUID.test(id)) };
  if (action === 'kick') payload.reason = values.reason ?? '';
  if (action === 'ban') {
    payload.minutes = Number.parseInt(values.minutes, 10) || 0;
    payload.reason = values.reason ?? '';
  }

  try {
    const result = await playerAction(state.instance, action, payload);

    // The command was written but the server never acknowledged it. Do not
    // claim the player is offline; refresh the ban list, which may have
    // changed anyway, and let the admin check the server.
    if (result?.warning) {
      state.bansLoaded = false;
      state.bansError = '';
      state.actions.setState({});
      notify(result.message || `${action} sent for ${player.name}, but the server did not confirm it.`, 'warn');
      return;
    }

    state.playerNotices.set(player.key, { action, at: Date.now() });
    // A ban adds an entry and an unban removes one, so the cached ban list is
    // stale either way: drop it and let the next render fetch it again.
    state.bansLoaded = false;
    state.bansError = '';
    // A kick or ban ends the session, so the row must stop claiming "online" —
    // the analysed log still ends while the player was connected.
    state.actions.setState({});
    notify(result?.message || `${action} sent for ${player.name}.`, 'ok');
  } catch (err) {
    notify(`${action} failed for ${player.name}: ${err.message}`, 'error');
  }
}

function actionBadge(notice) {
  return el(
    'span',
    {
      class: `action-badge ${notice.action}`,
      title: `${notice.action} sent at ${new Date(notice.at).toLocaleTimeString()}`,
    },
    notice.action,
  );
}

/** Unban one entry of the live ban list, by its ban-list id. */
async function runUnbanEntry(state, ban) {
  const who = ban.guid || ban.ip || `ban #${ban.id}`;
  const { ok } = await actionDialog({
    title: `Unban ${who}?`,
    description: ban.reason ? `Removes ban #${ban.id} — "${ban.reason}".` : `Removes ban #${ban.id} from the server.`,
    confirmLabel: 'Unban',
    danger: false,
  });
  if (!ok) return;

  try {
    const result = await playerAction(state.instance, 'unban', { name: who, banId: ban.id });
    // Whatever happened, the list on screen is now stale.
    state.bansLoaded = false;
    state.bansError = '';
    state.actions.setState({});
    notify(result?.message || `Ban #${ban.id} removed.`, result?.warning ? 'warn' : 'ok');
  } catch (err) {
    notify(`unban failed for ${who}: ${err.message}`, 'error');
  }
}

/** There is only one ban list per instance; fetch it once per report. */
function ensureBanList(state) {
  if (!state.instance || state.bansLoaded || state.bansLoading) return;

  state.bansLoading = true;
  fetchBans(state.instance)
    .then((bans) => {
      state.bans = bans;
      state.bansError = '';
    })
    .catch((err) => {
      state.bans = [];
      state.bansError = err.message;
    })
    .finally(() => {
      // Mark it as tried even on failure so a broken RCON does not turn every
      // render into another request.
      state.bansLoaded = true;
      state.bansLoading = false;
      state.actions.setState({});
    });
}

/** Actions column: kick/ban while online, unban only when actually banned. */
function playerActionsCell(state, player, online, ban) {
  const button = (label, className, action, title) =>
    el('button', {
      type: 'button',
      class: `btn tiny ${className}`,
      text: label,
      title: title ?? `${label} ${player.name} via RCON`,
      onClick: (event) => {
        // The row itself toggles selection; an action must not do both.
        event.stopPropagation();
        runPlayerAction(state, player, action);
      },
    });

  const buttons = [];
  if (online) buttons.push(button('Kick', 'ghost', 'kick'), button('Ban', 'danger', 'ban'));
  if (ban) {
    const why = ban.reason ? ` — ${ban.reason}` : '';
    const left = ban.minutes > 0 ? `, ${ban.minutes} min left` : ', permanent';
    buttons.push(button('Unban', 'ghost', 'unban', `Lift this ban (${ban.guid || ban.ip}${left}${why})`));
  }
  return el('td', { class: 'actions' }, ...buttons);
}

/**
 * The server's actual ban list. Ban entries carry BattlEye GUIDs, which do not
 * always appear in the ADM log, so this card is what makes every ban removable
 * independently of the player rows above it.
 */
function renderBansCard(panel, state) {
  const bans = state.bans ?? [];
  let body;

  if (!state.bansLoaded) {
    body = emptyState('Reading the ban list…');
  } else if (state.bansError) {
    body = emptyState(`Ban list unavailable: ${state.bansError}`);
  } else if (!bans.length) {
    body = emptyState('No active bans.');
  } else {
    const rows = bans.map((ban) =>
      el(
        'tr',
        {},
        el('td', { class: 'raw', text: ban.guid || ban.ip || '—' }),
        el('td', { class: 'num', text: ban.minutes > 0 ? `${fmtInt(ban.minutes)} min` : 'permanent' }),
        el('td', { text: ban.reason || '—' }),
        el(
          'td',
          { class: 'actions' },
          el('button', {
            type: 'button',
            class: 'btn tiny ghost',
            text: 'Unban',
            title: `Lift ban #${ban.id}`,
            onClick: (event) => {
              event.stopPropagation();
              runUnbanEntry(state, ban);
            },
          }),
        ),
      ),
    );
    body = table(
      [{ label: 'GUID / IP' }, { label: 'Time left', num: true }, { label: 'Reason' }, { label: 'Actions' }],
      rows,
    );
  }

  panel.append(
    card(
      `Active bans${state.bansLoaded && !state.bansError ? ` (${fmtInt(bans.length)})` : ''}`,
      state.bansLoaded && !state.bansError && bans.length
        ? el(
            'p',
            { class: 'small faint', style: 'margin:-4px 0 10px' },
            'Every ban on the server, read live over RCON. A ban cannot always be linked to a player row, so lift it here.',
          )
        : null,
      body,
    ),
  );
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

  // A player with nothing inside the Timeline window is left out of the list,
  // so it only offers people the other tabs can actually show.
  const window = timelineWindow(state);
  const inTimeline = adm.players.filter((p) => activeInTimeline(p, window));
  const hiddenCount = adm.players.length - inTimeline.length;

  const matches = inTimeline.filter((p) => !query || p.name.toLowerCase().includes(query));
  const players = onlySelected ? matches.filter((p) => selected.has(p.key)) : matches;

  // RCON actions need a configured RCON port/password on the instance.
  const rconReady = Boolean(state.instanceInfo?.rcon);
  const notices = state.playerNotices ?? new Map();

  // The ban list decides which rows get an Unban button; fetch it on first
  // render of this report. A player is banned when any SteamID from the log is
  // in the list (the ADM `id=` is a session token, not a SteamID).
  if (rconReady) ensureBanList(state);
  const banByGUID = new Map();
  for (const ban of state.bans ?? []) {
    if (ban.guid) banByGUID.set(String(ban.guid).toLowerCase(), ban);
  }
  const banFor = (p) => {
    for (const id of p.ids ?? []) {
      const ban = banByGUID.get(String(id).toLowerCase());
      if (ban) return ban;
    }
    return null;
  };

  // A kick or ban sent from this page ends the session, so the row must stop
  // claiming "online" even though the log still ends mid-session.
  const isLive = (p) => {
    const notice = notices.get(p.key);
    if (notice && (notice.action === 'kick' || notice.action === 'ban')) return false;
    return isOnline(p);
  };
  const onlineCount = players.filter(isLive).length;

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

  if (hiddenCount > 0) {
    panel.append(
      el(
        'p',
        { class: 'small faint', style: 'margin:-4px 0 12px' },
        `${fmtInt(hiddenCount)} player${hiddenCount === 1 ? '' : 's'} with nothing between ${window.label} hidden. `,
        el('button', {
          type: 'button',
          class: 'filter',
          text: 'Show the whole log',
          onClick: () => state.actions.setState({ timelineFrom: '', timelineTo: '' }),
        }),
      ),
    );
  }

  const rows = players.slice(0, 500).map((p) => {
    const online = isLive(p);
    const notice = notices.get(p.key);
    return el(
      'tr',
      {
        class: [selected.has(p.key) ? 'selected' : '', online ? 'online' : ''].filter(Boolean).join(' '),
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
      el(
        'td',
        {},
        el('span', { style: `color:${colorFor(p.name)}`, text: p.name }),
        online ? onlineBadge() : null,
        notice ? actionBadge(notice) : null,
      ),
      el('td', { class: 'num', text: fmtInt(p.connects) }),
      el('td', { class: 'num', text: fmtDuration(p.playtime) }),
      el('td', { class: 'num', text: fmtInt(p.kills) }),
      el('td', { class: 'num', text: fmtInt(p.deaths) }),
      el('td', { class: 'num', text: p.kd.toFixed(2) }),
      el('td', { class: 'num', text: fmtInt(p.chats) }),
      el('td', { class: 'num', text: fmtInt(p.builds) }),
      el('td', { class: 'num', text: p.suspicion ? fmtInt(p.suspicion) : '' }),
      rconReady ? playerActionsCell(state, p, online, banFor(p)) : null,
    );
  });

  // Bans first: a ban entry cannot always be linked to a log row (the ban list
  // uses BattlEye GUIDs that the ADM never records), so this card is the
  // reliable place to unban somebody. It must be impossible to miss.
  if (rconReady) renderBansCard(panel, state);

  const banCount = state.bans?.length ?? 0;
  panel.append(
    card(
      `Players (${fmtInt(players.length)})${onlineCount ? ` \u00b7 ${fmtInt(onlineCount)} online` : ''}`,
      banCount
        ? el(
            'p',
            { class: 'small faint', style: 'margin:-4px 0 10px' },
            `${fmtInt(banCount)} active ban${banCount === 1 ? '' : 's'} — use the Active bans card above to lift ` +
              (banCount === 1 ? 'it' : 'them') +
              '.',
          )
        : null,
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
              ...(rconReady ? [{ label: 'Actions' }] : []),
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
    { class: `card${isOnline(p) ? ' online' : ''}` },
    el('h3', {}, p.name, isOnline(p) ? onlineBadge() : null),
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

  const traps = adm.builds.filter((b) => b.trap).length;
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
        el('td', { text: b.trap ? 'Trap' : 'Build' }),
        el('td', { text: truncate(b.object, 90) }),
        el('td', {
          class: 'faint small',
          text: b.pos ? `<${b.pos.x.toFixed(0)}, ${b.pos.y.toFixed(0)}>` : '',
        }),
      ),
    );

  panel.append(
    card(
      `Build actions (${fmtInt(adm.builds.length)}${
        traps ? ` — ${fmtInt(traps)} trap${traps === 1 ? '' : 's'}` : ''
      })`,
      table(
        [
          { label: 'Time' },
          { label: 'Player' },
          { label: 'Type' },
          { label: 'Object' },
          { label: 'Position' },
        ],
        rows,
      ),
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

/** Epoch seconds → seconds since midnight, matching the log's own clock. */
function todOf(epoch) {
  if (!Number.isFinite(epoch)) return NaN;
  const rest = epoch % 86400;
  return rest < 0 ? rest + 86400 : rest;
}

/**
 * The Timeline tab's From/to window. `active` is false while both fields are
 * empty, which means "the whole log"; an overturned range (22:00 → 04:00) is a
 * window across midnight.
 */
function timelineWindow(state) {
  const from = parseClock(state.timelineFrom);
  const to = parseClock(state.timelineTo);
  const active = from !== null || to !== null;
  const start = from ?? 0;
  const end = to ?? 86399;
  const wrapped = from !== null && to !== null && from > to;
  const inside = (tod) => {
    if (!Number.isFinite(tod)) return false;
    if (!active) return true;
    if (start === end) return true;
    return wrapped ? tod >= start || tod <= end : tod >= start && tod <= end;
  };
  const label = active ? `${fmtClockInput(start)} – ${fmtClockInput(end)}` : 'the whole log';
  return { active, inside, label };
}

/** Whether a player did anything inside the timeline window. */
function activeInTimeline(player, window) {
  if (!window.active) return true;
  if (player.positions?.some((s) => window.inside(todOf(s.at)))) return true;
  return (player.sessions ?? []).some(
    (s) => window.inside(todOf(s.start)) || window.inside(todOf(s.end)),
  );
}

/**
 * A player is online when their last session was never closed: the log stopped
 * while they were still connected. Sessions arrive in time order, so the last
 * one carries the current state.
 */
function isOnline(player) {
  const sessions = player.sessions ?? [];
  const last = sessions[sessions.length - 1];
  return Boolean(last) && last.end === null;
}

function onlineBadge() {
  return el(
    'span',
    {
      class: 'online-badge',
      title: 'Still connected when the log ends \u2014 the last session was never closed',
    },
    el('span', { class: 'dot' }),
    'online',
  );
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
  const tooltip = el('div', { class: 'map-tooltip', hidden: true });
  const stage = el('div', { class: 'map-stage' }, canvas, tooltip);

  // ── event dots ───────────────────────────────────────────
  // renderMap records every drawn event as a hotspot; hovering one shows what
  // happened there, and a shared dot lists the players instead.
  const secondsOfDay = (at) => (Number.isFinite(at) ? ((at % 86400) + 86400) % 86400 : null);

  // Session events tell one story together: DayZ writes EmoteSitA right before
  // a normal logout, so the combination at a dot says whether the player logged
  // out here or came back to this spot. A missing emote proves nothing — logging
  // off inside a vehicle writes none — so a bare disconnect stays a disconnect.
  // Marker kinds differ from sample kinds (`login`/`logout` come from the Logins
  // layer, `connect`/`disconnect` from the position dots), so both spellings are
  // session events.
  const SESSION_KINDS = new Set([
    'connect',
    'login',
    'disconnect',
    'logout',
    'logoutKick',
    'kick',
    'emote',
    'state',
    'last',
  ]);

  const sessionVerdict = (items) => {
    const kinds = new Set(items.map((i) => i.kind));
    // A kick is the one end-of-session cause the log states outright.
    if (kinds.has('kick') || kinds.has('logoutKick')) return 'kicked';

    const times = (match) =>
      items.filter((i) => match(i.kind) && Number.isFinite(i.at)).map((i) => i.at);
    const connects = times((k) => k === 'connect' || k === 'login');
    const disconnects = times((k) => k === 'disconnect' || k === 'logout' || k === 'logoutKick');
    const emote = kinds.has('emote') || kinds.has('state');

    // A connect after a disconnect means the player came back to this spot.
    const cameBack =
      connects.length > 0 && disconnects.length > 0 && Math.max(...connects) > Math.min(...disconnects);
    if (cameBack) return 'login';
    // Emote then disconnect is the classic logout.
    if (disconnects.length) return emote ? (kinds.has('last') ? 'last position' : 'logout') : 'disconnect';
    if (connects.length) return 'login';
    return null;
  };

  const describeSpot = (spot) => {
    if (spot.items.length === 1) {
      const item = spot.items[0];
      const tod = secondsOfDay(item.at);
      return tod === null ? item.text : `${fmtClock(tod)} · ${item.text}`;
    }
    const names = [...new Set(spot.items.flatMap((i) => i.players).filter(Boolean))];

    // A dot made only of session events is one story, not a list of events.
    if (spot.items.every((i) => SESSION_KINDS.has(i.kind))) {
      const verdict = sessionVerdict(spot.items);
      if (verdict) return `${names.join(', ')} — ${verdict}`;
    }

    // Otherwise list what happened instead of only who was involved.
    const details = [...new Set(spot.items.map((i) => i.text).filter(Boolean))];
    if (details.length) {
      // One player's events repeat the name in every entry; show it once.
      const solo = names.length === 1 ? names[0] : null;
      const lines =
        solo !== null
          ? details.map((d) => (d.startsWith(`${solo} `) ? d.slice(solo.length + 1) : d))
          : details;
      const shown = lines.slice(0, 3).join('; ');
      const more = lines.length > 3 ? `; +${lines.length - 3} more` : '';
      const who = solo !== null ? `${solo}: ` : '';
      return `${fmtInt(spot.items.length)} events · ${who}${shown}${more}`;
    }
    return `${fmtInt(spot.items.length)} events · ${names.join(', ')}`;
  };

  const showTooltip = (spot) => {
    if (!spot) {
      tooltip.hidden = true;
      return;
    }
    tooltip.textContent = describeSpot(spot);
    tooltip.hidden = false;
    // Keep it inside the stage: flip left/up when the dot is near an edge.
    const maxX = Math.max(8, stage.clientWidth - tooltip.offsetWidth - 10);
    const maxY = Math.max(8, stage.clientHeight - tooltip.offsetHeight - 10);
    tooltip.style.left = `${Math.min(Math.max(8, spot.x + 14), maxX)}px`;
    tooltip.style.top = `${Math.min(Math.max(8, spot.y - tooltip.offsetHeight - 12), maxY)}px`;
  };

  // Generous radius: the dots are small, and asking for a pixel-perfect hit
  // makes the tooltip feel broken on a trackpad.
  const HOVER_RADIUS = 18;

  const hoverAt = (clientX, clientY) => {
    const list = view.hotspots || [];
    const rect = canvas.getBoundingClientRect();
    const x = clientX - rect.left;
    const y = clientY - rect.top;
    let best = null;
    let bestDist = HOVER_RADIUS * HOVER_RADIUS;
    for (const spot of list) {
      const dist = (spot.x - x) ** 2 + (spot.y - y) ** 2;
      if (dist <= bestDist) {
        bestDist = dist;
        best = spot;
      }
    }
    const key = best ? best.key : null;
    canvas.classList.toggle('over-dot', Boolean(best));
    if (key !== mapState.hoverKey) {
      mapState.hoverKey = key;
      view.hoverKey = key;
      repaintMap();
    }
    showTooltip(best);
    return Boolean(best);
  };

  // ── zoom & pan ───────────────────────────────────────────
  // Wheel (and Safari pinch) zooms about the pointer, double-click zooms in at
  // the pointer, dragging pans, **Fit**/returns to 1× where the map's width
  // matches the frame exactly. Zooming out below 1× is not allowed, so the
  // frame never shows empty bands on the sides.
  const zoomLabel = el('span', { class: 'map-zoom-value', text: '1.0×' });

  const syncZoomLabel = () => {
    zoomLabel.textContent = `${(mapState.zoom || 1).toFixed(1)}×`;
  };
  syncZoomLabel();

  // The renderer may clamp the zoom to the fit scale after a resize; keep the
  // stored zoom and the readout on the value that was actually drawn.
  view.onViewport = (vp) => {
    mapState.zoom = vp.zoom;
    syncZoomLabel();
  };

  // `view` is the snapshot the renderer draws, so zoom and pan have to be copied
  // into it as well, not only into mapState.
  const applyViewport = () => {
    view.zoom = mapState.zoom;
    view.centerX = mapState.centerX;
    view.centerY = mapState.centerY;
    repaintMap();
  };

  // Zooming in past the deepest tile level just stretches pixels, so the
  // ceiling follows the map's own pyramid. Two and a half steps of upscaling is
  // the point where roads and buildings stop being legible; going further only
  // makes the map blurry, which is exactly what a tile viewer should avoid.
  // Zoom 1 already fits the map's width to the canvas.
  const zoomCeiling = () => {
    const vp = view.viewport;
    const tileMax = state.mapMeta?.maxZoom;
    if (!vp) return MAX_MAP_ZOOM;
    if (!tileMax) return 16; // no imagery: the grid has nothing to resolve
    const limit = (2.5 * 2 ** tileMax * 256) / Math.max(1, vp.width);
    return Math.max(2, Math.min(MAX_MAP_ZOOM, limit));
  };

  const zoomTo = (nextZoom, anchorX = null, anchorY = null) => {
    const vp = view.viewport;
    // Before the first paint there is no viewport to anchor to; keep the
    // requested zoom anyway so early clicks are not lost.
    if (!vp) {
      mapState.zoom = Math.max(1, Math.min(zoomCeiling(), nextZoom));
      syncZoomLabel();
      applyViewport();
      return;
    }
    // Zoom 1 fits the map's width; it cannot be zoomed out past that.
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
      vp.height,
      worldX - (ax - vp.width / 2) / kNext,
      worldY + (ay - vp.height / 2) / kNext,
    );
    mapState.zoom = clamped.zoom;
    mapState.centerX = clamped.cx;
    mapState.centerY = clamped.cy;
    syncZoomLabel();
    applyViewport();
  };

  // Back to width fit, centred: the widest the map can be shown.
  const zoomToFit = () => {
    mapState.zoom = 1;
    mapState.centerX = null;
    mapState.centerY = null;
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
    tooltip.hidden = true;
    canvas.setPointerCapture(event.pointerId);
    canvas.classList.add('dragging');
  });

  canvas.addEventListener('pointermove', (event) => {
    if (!drag) {
      hoverAt(event.clientX, event.clientY);
      return;
    }
    if (!view.viewport) return;
    const vp = view.viewport;
    const moved = clampView(
      vp.world,
      vp.zoom,
      vp.width,
      vp.height,
      drag.cx - (event.clientX - drag.x) / vp.k,
      drag.cy + (event.clientY - drag.y) / vp.k,
    );
    mapState.centerX = moved.cx;
    mapState.centerY = moved.cy;
    applyViewport();
  });

  canvas.addEventListener('pointerup', endDrag);
  canvas.addEventListener('pointercancel', endDrag);
  canvas.addEventListener('pointerleave', () => {
    endDrag();
    tooltip.hidden = true;
    canvas.classList.remove('over-dot');
    if (mapState.hoverKey) {
      mapState.hoverKey = null;
      view.hoverKey = null;
      repaintMap();
    }
  });

  canvas.addEventListener('wheel', (event) => {
      if (!view.viewport) return;
      event.preventDefault();
      const rect = canvas.getBoundingClientRect();
      // deltaY is in pixels in Chrome/Safari but in *lines* (or pages) in
      // Firefox and on most real mice. Without this the factor stays ~0.995
      // per notch there and the map looks like it refuses to zoom.
      const vp = view.viewport;
      const unit = event.deltaMode === 1 ? 24 : event.deltaMode === 2 ? vp.height : 1;
      const delta = Math.max(-400, Math.min(400, event.deltaY * unit));
      const factor = Math.exp(-delta * 0.0015);
      zoomTo((mapState.zoom || 1) * factor, event.clientX - rect.left, event.clientY - rect.top);
    },
    { passive: false },
  );

  // Double-click zooms in where the pointer is; **Fit** is the way back out.
  canvas.addEventListener('dblclick', (event) => {
    const rect = canvas.getBoundingClientRect();
    zoomTo((mapState.zoom || 1) * 1.6, event.clientX - rect.left, event.clientY - rect.top);
  });

  // Safari reports trackpad pinch as gesture events (not wheel), so hook them
  // too or pinch-zooming the map does nothing there.
  let gestureZoom = null;
  canvas.addEventListener('gesturestart', (event) => {
    event.preventDefault();
    gestureZoom = mapState.zoom || 1;
  });
  canvas.addEventListener('gesturechange', (event) => {
    event.preventDefault();
    if (gestureZoom === null) return;
    const rect = canvas.getBoundingClientRect();
    zoomTo(gestureZoom * (event.scale || 1), event.clientX - rect.left, event.clientY - rect.top);
  });
  canvas.addEventListener('gestureend', (event) => {
    event.preventDefault();
    gestureZoom = null;
  });

  // Player picker: any combination of players can be ticked, instead of
  // all-or-one. The set is shared with the Players tab, so ticks made there
  // show up here and vice versa. Ticking repaints the canvas directly rather
  // than re-rendering the panel, so the popover stays open while picking.
  const repaintMap = () => {
    if (canvas.isConnected) renderMap(canvas, report, view);
  };
  const chosen = state.selectedPlayers;
  const period = adm.period || {};
  let poolSize = adm.players.length;

  // Same idea as the Players tab: a player with no position inside the map's
  // timeline window cannot be drawn, so the list leaves them out — except the
  // ones already ticked, which must stay untickable. The window moves while the
  // slider is dragged, so the pool is recomputed on every fill.
  const pickerPool = () => {
    const fullRange =
      Number.isFinite(mapState.windowFrom) &&
      Number.isFinite(mapState.windowTo) &&
      Number.isFinite(period.from) &&
      Number.isFinite(period.to) &&
      mapState.windowFrom <= period.from &&
      mapState.windowTo >= period.to;
    if (!Number.isFinite(mapState.windowFrom) || !Number.isFinite(mapState.windowTo) || fullRange) {
      return adm.players;
    }
    return adm.players.filter(
      (p) =>
        chosen.has(p.key) ||
        p.positions.some((s) => Number.isFinite(s.at) && s.at >= mapState.windowFrom && s.at <= mapState.windowTo),
    );
  };

  const summaryText = () =>
    chosen.size
      ? `${fmtInt(chosen.size)} of ${fmtInt(poolSize)} selected`
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
    const pool = pickerPool();
    poolSize = pool.length;
    pickerSummary.textContent = summaryText();
    const matches = pool.filter((p) => !query || p.name.toLowerCase().includes(query));
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

    // The duration text changes length from one slider step to the next
    // ("(6h 2m)" → "(5h 55m)" → "(59m 59s)"), and it is the flexible slider
    // track beside it that absorbs the difference — the track would visibly
    // shrink and grow while a thumb is dragged. Reserving the width of the
    // longest label this period can print keeps it still: minutes and seconds
    // are always two digits, and the hours can reach the whole period.
    const measureLabel = (text) => {
      const probe = el('span', { class: 'map-window-value', text });
      probe.style.cssText = 'position:absolute;left:-10000px;top:0;white-space:nowrap;';
      document.body.append(probe);
      const width = probe.getBoundingClientRect().width;
      probe.remove();
      return width;
    };
    const hourDigits = '9'.repeat(String(Math.floor((hi - lo) / 3600)).length);
    const widestLabel = Math.max(
      measureLabel(`${stamp(lo)} → ${stamp(hi)} (59m 59s)`),
      measureLabel(`${stamp(lo)} → ${stamp(hi)} (${hourDigits}h 59m)`),
    );
    valueText.style.minWidth = `${Math.ceil(widestLabel)}px`;

    // Both thumbs share one track, so they may not touch: the upper input
    // swallows the drag where they overlap, leaving the lower thumb
    // ungrabbable, and a drag would otherwise cross the other side. The window
    // is therefore kept a thumb wide, in slider units, at all times.
    const THUMB_PX = 18;
    const minGap = () => {
      const width = fromInput.getBoundingClientRect().width;
      if (!width) return step;
      return Math.max(step, Math.ceil(((THUMB_PX / width) * (hi - lo)) / step) * step);
    };
    const keepApart = () => {
      const gap = minGap();
      if (mapState.windowTo - mapState.windowFrom >= gap) return;
      mapState.windowTo = Math.min(hi, mapState.windowFrom + gap);
      mapState.windowFrom = Math.max(lo, mapState.windowTo - gap);
    };

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
        // Stops at the other thumb instead of sliding under it.
        mapState.windowFrom = Math.min(Number(fromInput.value), mapState.windowTo - minGap());
        paint();
        fillPicker();
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
        // Stops at the other thumb instead of sliding under it.
        mapState.windowTo = Math.max(Number(toInput.value), mapState.windowFrom + minGap());
        paint();
        fillPicker();
        repaintMap();
      },
    });

    keepApart();
    paint();

    // One click back to "just now": the window opens on the last six hours,
    // and these presets are the quickest way to widen or narrow it again.
    const preset = (label, span, title) =>
      el('button', {
        type: 'button',
        class: 'filter',
        text: label,
        title,
        onClick: () => {
          mapState.windowTo = hi;
          mapState.windowFrom = span === null ? lo : Math.max(lo, hi - span);
          keepApart();
          paint();
          fillPicker();
          repaintMap();
        },
      });

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
      el(
        'div',
        { class: 'map-window-presets' },
        preset('Last 1h', 3600, 'Show the last hour of the log'),
        preset('6h', 21600, 'Show the last 6 hours of the log'),
        preset('24h', 86400, 'Show the last 24 hours of the log'),
        preset('Whole period', null, 'Reset the window to the whole analysed period'),
      ),
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
              title: 'Back to the width fit (zoom 1×)',
              onClick: () => zoomToFit(),
            }),
          ),
        ),
      ),
      windowControl,
      el(
        'div',
        { class: 'filters' },
        layerToggle('kills', 'Kills'),
        layerToggle('deaths', 'Dead'),
        layerToggle('trails', 'Trails'),
        layerToggle('builds', 'Bases'),
        layerToggle('heat', 'Heatmap'),
        layerToggle('connects', 'Last positions'),
        layerToggle('logins', 'Logins'),
        layerToggle('jumps', 'Position jumps'),
      ),
      stage,
      el(
        'div',
        { class: 'legend' },
        el('span', {}, el('i', { style: 'background:#e5484d' }), 'Kill position'),
        el('span', {}, el('i', { style: 'background:#dfe5ec' }), 'Death'),
        el('span', {}, el('i', { style: 'background:#5ac8dc' }), 'Build action'),
        el('span', {}, el('i', { style: 'background:#d46bd8' }), 'Trap'),
        el('span', {}, el('i', { style: 'background:#4cc38a' }), 'Login'),
        el('span', {}, el('i', { style: 'background:#a7b4c4' }), 'Logout'),
        el('span', {}, el('i', { style: 'background:#e5484d' }), 'Kicked'),
        el('span', {}, el('i', { style: 'background:var(--warning)' }), 'Impossible movement'),
      ),
      el('p', { class: 'small faint map-hint', text: mapHint(mapMeta, mapState, state) }),
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
