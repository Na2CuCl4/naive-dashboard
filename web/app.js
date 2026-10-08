import {t, locale, setLocale, formatBytes, formatDate, formatNumber, resources} from './i18n.js';

const app = document.querySelector('#app');
const dialog = document.querySelector('#editor');
const notifications = document.querySelector('#notifications');
const tabs = ['overview', 'users', 'nodes', 'usage'];
let csrf = '';
let authenticated = false;
let state = {users: [], nodes: [], usage: []};
let currentTab = tabs.includes(location.hash.slice(1)) ? location.hash.slice(1) : 'overview';
let search = '';
let usageUser = '';
let usageNode = '';
let usageMonth = '';
let refreshing = false;
let modalRender = null;
let lastRefresh = null;
let requestInProgress = false;

const paths = {
  overview: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  users: '<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2m20 0v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75"/><circle cx="9" cy="7" r="4"/>',
  nodes: '<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.01M7 17.5h.01M11 6.5h6M11 17.5h6"/>',
  usage: '<path d="M3 3v18h18M7 14l4-4 4 3 6-8"/>',
  refresh: '<path d="M20 7v5h-5M4 17v-5h5"/><path d="M6.1 6a7 7 0 0 1 11.55-.7L20 8M4 16l2.35 2.7A7 7 0 0 0 17.9 18"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  arrow: '<path d="M5 12h14m-5-5 5 5-5 5"/>',
  up: '<path d="M12 19V5m-5 5 5-5 5 5"/>',
  down: '<path d="M12 5v14m-5-5 5 5 5-5"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  shield: '<path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6z"/><path d="m8 12 3 3 5-6"/>',
  logout: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4m7 14 5-5-5-5m5 5H9"/>',
  search: '<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>',
  close: '<path d="m6 6 12 12M6 18 18 6"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c5 5 5 13 0 18-5-5-5-13 0-18Z"/>',
  link: '<path d="m10 13 4-4M9 15l-2 2a3.54 3.54 0 0 1-5-5l4-4a3.54 3.54 0 0 1 5 0m2 1 2-2a3.54 3.54 0 0 1 5 5l-4 4a3.54 3.54 0 0 1-5 0"/>',
  alert: '<path d="m10.3 3.8-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.7-3.2l-8-14a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4m0 4h.01"/>',
};
function icon(name, extra = '') { return `<svg class="icon ${extra}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[name] || paths.overview}</svg>`; }
function esc(value) { return String(value ?? '').replace(/[&<>"']/g, char => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'}[char])); }
function label(key) { return esc(t(key)); }
function messageFor(code) { return resources[locale()][`error_${code}`] || t('unexpectedError'); }
function sumUsage(user) { return (Number(user.total_up) || 0) + (Number(user.total_down) || 0); }
function isPending(node) { return Number(node.desired_version) !== Number(node.applied_version); }
function nodeName(id) { return state.nodes.find(node => node.id === id)?.name || id; }
function userName(id) { return state.users.find(user => user.id === id)?.name || id; }
function subscriptionURL(user) { return `${location.origin}/sub/${encodeURIComponent(user.subscription_token)}`; }
function expired(user) { return Boolean(user.expires_at && new Date(user.expires_at).getTime() <= Date.now()); }
function userStatus(user) { return !user.enabled ? 'disabled' : expired(user) ? 'expired' : user.quota_state === 'exceeded' ? 'exceeded' : 'enabled'; }
function health(node) { return ['healthy', 'offline', 'unknown', 'error'].includes(node.health) ? node.health : 'error'; }
function badge(status) { return `<span class="badge badge-${esc(status)}"><span class="status-dot"></span>${label(status)}</span>`; }
function button(action, key, id = '', classes = 'button button-secondary', extra = '') { return `<button type="button" class="${classes}" data-action="${action}"${id ? ` data-id="${esc(id)}"` : ''} ${extra}>${label(key)}</button>`; }
function languagePicker() { return `<label class="language-picker">${icon('globe')}<span class="sr-only">${label('language')}</span><select data-language aria-label="${label('language')}"><option value="zh-CN"${locale() === 'zh-CN' ? ' selected' : ''}>简体中文</option><option value="en"${locale() === 'en' ? ' selected' : ''}>English</option></select></label>`; }
function empty(iconName, title, detail, action = '') { return `<div class="empty-state"><span class="empty-icon">${icon(iconName)}</span><h3>${label(title)}</h3><p>${label(detail)}</p>${action}</div>`; }
function notify(key, bad = false) {
  const toast = document.createElement('div');
  toast.className = `toast${bad ? ' toast-error' : ''}`;
  toast.innerHTML = `${icon(bad ? 'alert' : 'check')}<span>${esc(key)}</span>`;
  notifications.append(toast);
  setTimeout(() => toast.remove(), 5500);
}

async function api(path, options = {}) {
  let response;
  try {
    response = await fetch(path, {
      ...options,
      credentials: 'same-origin',
      headers: {'Content-Type': 'application/json', ...(csrf ? {'X-CSRF-Token': csrf} : {}), ...options.headers},
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    });
  } catch { throw new Error(t('connectionError')); }
  let body = {};
  try { body = await response.json(); } catch { /* Empty success bodies are allowed. */ }
  if (!response.ok) {
    if (response.status === 401 && authenticated) {
      authenticated = false;
      csrf = '';
      closeDialog();
      render();
    }
    throw new Error(messageFor(body.error));
  }
  return body;
}

function render() {
  setLocale(locale());
  app.removeAttribute('aria-busy');
  if (!authenticated) { renderLogin(); return; }
  const primaryAction = currentTab === 'users' ? `<button class="button button-primary" data-action="add-user">${icon('plus')}${label('addUser')}</button>` : currentTab === 'nodes' ? `<button class="button button-primary" data-action="add-node">${icon('plus')}${label('addNode')}</button>` : '';
  app.innerHTML = `<div class="dashboard-shell">
    <aside class="sidebar"><a class="brand" href="#overview"><span class="brand-symbol">N<span></span></span><span><strong>Naive</strong><small>Dashboard</small></span></a>
      <div class="sidebar-caption">${label('privateCluster')}</div>
      <nav aria-label="${label('appName')}">${tabs.map(tab => `<a href="#${tab}" class="nav-link${currentTab === tab ? ' active' : ''}"${currentTab === tab ? ' aria-current="page"' : ''}>${icon(tab)}<span>${label(tab)}</span>${tab === 'nodes' && state.nodes.length ? `<span class="nav-count">${state.nodes.length}</span>` : ''}</a>`).join('')}</nav>
      <div class="sidebar-footer"><div class="sidebar-service">${icon('shield')}<div>${label('nativeNaive')}<small>${label('secureControl')}</small></div></div><button class="logout-button" data-action="logout">${icon('logout')}${label('logout')}</button></div>
    </aside>
    <div class="workspace"><header class="topbar"><div class="breadcrumb">${label('appName')}<span>/</span><strong>${label(currentTab)}</strong></div><div class="topbar-tools">${languagePicker()}<span class="admin-avatar" title="${label('admin')}" aria-label="${label('admin')}">A</span><button class="icon-button topbar-logout" data-action="logout" aria-label="${label('logout')}" title="${label('logout')}">${icon('logout')}</button></div></header>
      <main id="main"><section class="page-heading"><div><div class="eyebrow">${label('privateCluster')}</div><h1>${label(`${currentTab}Title`)}</h1><p>${label(`${currentTab}Intro`)}</p></div><div class="heading-actions"><button class="button button-secondary" data-action="refresh"${refreshing ? ' disabled' : ''}>${icon('refresh', refreshing ? 'spin' : '')}<span>${label(refreshing ? 'refreshing' : 'refresh')}</span></button>${primaryAction}</div></section>
      <div class="page-content">${currentTab === 'overview' ? renderOverview() : currentTab === 'users' ? renderUsers() : currentTab === 'nodes' ? renderNodes() : renderUsage()}</div>
      <footer class="page-footer"><span>${icon('clock')}${label('automaticSync')}</span><span>${lastRefresh ? esc(t('refreshed', {time: formatDate(lastRefresh)})) : ''}</span></footer></main></div></div>`;
}

function renderLogin() {
  app.innerHTML = `<div class="login-layout"><section class="login-hero"><a class="brand" href="#overview"><span class="brand-symbol">N<span></span></span><span><strong>Naive</strong><small>Dashboard</small></span></a><div class="hero-content"><div class="eyebrow">${label('privateCluster')}</div><h1>${esc(t('loginHero')).replace('\n', '<br>')}</h1><p>${label('loginHeroDetail')}</p><div class="hero-network" aria-hidden="true"><div class="network-center">${icon('shield')}</div><div class="network-node node-one">${icon('nodes')}</div><div class="network-node node-two">${icon('nodes')}</div><div class="network-node node-three">${icon('nodes')}</div><span class="network-orbit orbit-one"></span><span class="network-orbit orbit-two"></span></div></div><div class="hero-footer">${icon('check')}${label('nativeNaive')}<span>·</span>${label('secureControl')}</div></section><section class="login-main"><div class="login-language">${languagePicker()}</div><div class="login-card"><div class="login-badge">${icon('shield')}</div><h2>${label('loginTitle')}</h2><p>${label('loginIntro')}</p><form id="login-form" novalidate><label class="field"><span>${label('password')}</span><input name="password" type="password" autocomplete="current-password" required autofocus maxlength="1024"></label><div class="form-error" role="alert" hidden></div><button class="button button-primary login-submit" type="submit">${label('login')}${icon('arrow')}</button></form><p class="login-note">${label('loginNote')}</p></div><div class="login-copyright">Naive Dashboard</div></section></div>`;
}

function renderOverview() {
  const up = state.usage.reduce((total, row) => total + (Number(row.up) || 0), 0);
  const down = state.usage.reduce((total, row) => total + (Number(row.down) || 0), 0);
  const metrics = [
    ['users', 'totalUsers', formatNumber(state.users.length), t('enabledUsers', {count: state.users.filter(user => userStatus(user) === 'enabled').length})],
    ['nodes', 'healthyNodes', formatNumber(state.nodes.filter(node => health(node) === 'healthy').length), t('ofNodes', {count: state.nodes.length})],
    ['usage', 'totalTransfer', formatBytes(up + down), `${t('upload')} ${formatBytes(up)} · ${t('download')} ${formatBytes(down)}`],
    ['clock', 'pendingNodes', formatNumber(state.nodes.filter(isPending).length), t('pendingDescription')],
  ];
  const alerts = state.users.filter(user => ['near', 'exceeded'].includes(user.quota_state));
  return `<div class="metrics-grid">${metrics.map(([symbol, title, value, description]) => `<article class="metric-card"><div class="metric-label">${label(title)}<span class="metric-icon">${icon(symbol)}</span></div><div class="metric-value">${esc(value)}</div><div class="metric-detail">${esc(description)}</div></article>`).join('')}</div>
    ${state.nodes.some(node => health(node) === 'offline') ? `<div class="notice notice-warning">${icon('alert')}<span>${label('offlineNotice')}</span></div>` : ''}
    <div class="overview-grid"><section class="panel"><div class="panel-heading"><h2>${label('nodeHealth')}</h2><a class="text-link" href="#nodes">${label('viewAll')}${icon('arrow')}</a></div>${state.nodes.length ? `<div class="node-summary-list">${state.nodes.slice(0, 5).map(node => `<div class="node-summary"><span class="node-avatar">${icon('nodes')}</span><div class="node-summary-name"><strong>${esc(node.name)}</strong><small>${esc(node.proxy_host)}:${esc(node.proxy_port)}</small></div><div class="node-summary-status">${badge(health(node))}<small>${label(isPending(node) ? 'pending' : 'inSync')}</small></div></div>`).join('')}</div>` : empty('nodes', 'noNodes', 'noNodesDetail', button('add-node', 'addNode', '', 'button button-primary'))}</section>
    <section class="panel"><div class="panel-heading"><h2>${label('recentUsage')}</h2><a class="text-link" href="#usage">${label('viewAll')}${icon('arrow')}</a></div>${state.users.length ? `<div class="usage-summary-list">${[...state.users].sort((a, b) => sumUsage(b) - sumUsage(a)).slice(0, 5).map(user => `<div class="usage-summary"><div class="row-spread"><strong>${esc(user.name)}</strong><span>${esc(formatBytes(sumUsage(user)))}</span></div>${quotaMeter(user)}</div>`).join('')}</div>` : empty('users', 'noUsers', 'noUsersDetail', button('add-user', 'addUser', '', 'button button-primary'))}</section></div>
    <section class="panel quota-panel"><div class="panel-heading"><h2>${label('quotaAlerts')}</h2>${alerts.length ? `<span class="count-chip">${alerts.length}</span>` : icon('check')}</div>${alerts.length ? alerts.map(user => `<div class="quota-alert-row"><div><strong>${esc(user.name)}</strong><span>${esc(t('quotaUsed', {used: formatBytes(sumUsage(user)), limit: formatBytes(user.quota_bytes)}))}</span></div>${badge(user.quota_state)}${button('edit-user', 'edit', user.id, 'button button-small button-secondary')}</div>`).join('') : `<p class="quiet-message">${label('quotaAlertsNone')}</p>`}<div class="panel-note">${icon('shield')}<span>${label('softQuotaNote')}</span></div></section>`;
}

function quotaMeter(user) {
  const percent = Number(user.quota_bytes) > 0 ? Math.min(100, sumUsage(user) / user.quota_bytes * 100) : 0;
  return `<progress class="quota-track quota-${esc(user.quota_state || 'normal')}" value="${percent}" max="100" aria-label="${label('quota')}">${percent}%</progress><small class="quota-caption">${user.quota_bytes > 0 ? esc(t('quotaUsed', {used: formatBytes(sumUsage(user)), limit: formatBytes(user.quota_bytes)})) : label('unlimited')}</small>`;
}

function renderUsers() {
  if (!state.users.length) return `<section class="panel">${empty('users', 'noUsers', 'noUsersDetail', button('add-user', 'addUser', '', 'button button-primary'))}</section>`;
  const visible = state.users.filter(user => user.name.toLowerCase().includes(search.toLowerCase()));
  return `<section class="panel"><div class="list-toolbar">${searchField('searchUsers')}<span class="list-count">${state.users.length} ${label('users')}</span></div>${visible.length ? `<div class="table-scroll"><table><thead><tr><th>${label('user')}</th><th>${label('status')}</th><th>${label('access')}</th><th>${label('quota')}</th><th>${label('expiration')}</th><th class="align-right">${label('actions')}</th></tr></thead><tbody>${visible.map(user => `<tr><td><div class="identity-cell"><span class="user-avatar">${esc(user.name.slice(0, 1).toUpperCase())}</span><strong>${esc(user.name)}</strong></div></td><td>${badge(userStatus(user))}</td><td><div class="grant-tags">${user.grants?.length ? user.grants.map(id => `<span class="grant-tag">${esc(nodeName(id))}</span>`).join('') : `<span class="muted">${label('noAccess')}</span>`}</div></td><td class="quota-cell"><strong>${esc(formatBytes(sumUsage(user)))}</strong>${quotaMeter(user)}</td><td class="nowrap muted">${user.expires_at ? esc(formatDate(user.expires_at)) : label('noExpiry')}</td><td><div class="table-actions">${button('subscription', 'subscription', user.id, 'button button-small button-secondary')}${button('edit-user', 'edit', user.id, 'button button-small button-ghost')}</div></td></tr>`).join('')}</tbody></table></div>` : empty('search', 'noResults', 'noResultsDetail')}</section><div class="notice">${icon('shield')}<span>${label('softQuotaNote')}</span></div>`;
}

function searchField(placeholder) { return `<label class="search-field">${icon('search')}<input data-search type="search" placeholder="${label(placeholder)}" aria-label="${label(placeholder)}" value="${esc(search)}"></label>`; }

function renderNodes() {
  if (!state.nodes.length) return `<section class="panel">${empty('nodes', 'noNodes', 'noNodesDetail', button('add-node', 'addNode', '', 'button button-primary'))}</section>`;
  const visible = state.nodes.filter(node => `${node.name} ${node.host} ${node.proxy_host}`.toLowerCase().includes(search.toLowerCase()));
  return `<div class="standalone-toolbar">${searchField('searchNodes')}<span class="list-count">${state.nodes.length} ${label('nodes')}</span></div>${visible.length ? `<div class="nodes-grid">${visible.map(node => `<article class="panel node-card"><div class="node-card-heading"><span class="node-avatar">${icon('nodes')}</span><div><h2>${esc(node.name)}</h2><span class="node-protocol">Naive · ${esc(String(node.protocol || 'https').toUpperCase())}</span></div>${badge(health(node))}</div><dl class="node-details"><div><dt>${label('endpoint')}</dt><dd>${esc(node.proxy_host)}:${esc(node.proxy_port)}</dd></div><div><dt>${label('configuration')}</dt><dd><span class="version-pair" title="${label('appliedVersion')} / ${label('desiredVersion')}">v${esc(node.applied_version || 0)} / v${esc(node.desired_version || 0)}</span><span class="sync-state${isPending(node) ? ' pending-state' : ''}">${label(isPending(node) ? 'pending' : 'inSync')}</span></dd></div><div><dt>${label('lastSeen')}</dt><dd>${esc(formatDate(node.last_seen))}</dd></div><div><dt>${label('lastUsage')}</dt><dd>${esc(formatDate(node.last_usage))}</dd></div><div><dt>${label('lastEventAt')}</dt><dd>${esc(formatDate(node.last_event_at))}</dd></div></dl>${node.usage_backlog ? `<div class="node-backlog">${icon('clock')}<span>${label('usageBacklog')}</span></div>` : ''}${node.error_code ? `<div class="node-error">${icon('alert')}<span>${esc(messageFor(node.error_code))}</span></div>` : ''}<div class="node-card-actions">${button('sync', 'sync', node.id, 'button button-small button-secondary')}${button('collect', 'collect', node.id, 'button button-small button-ghost')}${button('edit-node', 'edit', node.id, 'button button-small button-ghost')}</div></article>`).join('')}</div>` : `<section class="panel">${empty('search', 'noResults', 'noResultsDetail')}</section>`}`;
}

function renderUsage() {
  const months = [...new Set(state.usage.map(row => row.month).filter(Boolean))].sort().reverse();
  const selected = state.usage.filter(row => (!usageUser || row.user_id === usageUser) && (!usageNode || row.node_id === usageNode) && (!usageMonth || row.month === usageMonth));
  const grouped = new Map();
  for (const row of selected) {
    const key = JSON.stringify([row.user_id, row.node_id]);
    const existing = grouped.get(key) || {user_id: row.user_id, node_id: row.node_id, up: 0, down: 0};
    existing.up += Number(row.up) || 0;
    existing.down += Number(row.down) || 0;
    grouped.set(key, existing);
  }
  const rows = [...grouped.values()].sort((a, b) => (b.up + b.down) - (a.up + a.down));
  const up = rows.reduce((total, row) => total + row.up, 0);
  const down = rows.reduce((total, row) => total + row.down, 0);
  return `<div class="usage-totals"><div><span class="transfer-icon">${icon('up')}</span><span><small>${label('upload')}</small><strong>${esc(formatBytes(up))}</strong></span></div><div><span class="transfer-icon download-icon">${icon('down')}</span><span><small>${label('download')}</small><strong>${esc(formatBytes(down))}</strong></span></div><div><span class="transfer-icon total-icon">${icon('usage')}</span><span><small>${label('total')}</small><strong>${esc(formatBytes(up + down))}</strong></span></div></div><section class="panel"><div class="list-toolbar usage-filters"><select data-usage-user aria-label="${label('user')}"><option value="">${label('allUsers')}</option>${state.users.map(user => `<option value="${esc(user.id)}"${usageUser === user.id ? ' selected' : ''}>${esc(user.name)}</option>`).join('')}</select><select data-usage-node aria-label="${label('node')}"><option value="">${label('allNodes')}</option>${state.nodes.map(node => `<option value="${esc(node.id)}"${usageNode === node.id ? ' selected' : ''}>${esc(node.name)}</option>`).join('')}</select><select data-usage-month aria-label="${label('month')}"><option value="">${label('allMonths')}</option>${months.map(month => `<option value="${esc(month)}"${usageMonth === month ? ' selected' : ''}>${esc(month)}</option>`).join('')}</select></div>${rows.length ? `<div class="table-scroll"><table><thead><tr><th>${label('user')}</th><th>${label('node')}</th><th class="align-right">${label('upload')}</th><th class="align-right">${label('download')}</th><th class="align-right">${label('total')}</th></tr></thead><tbody>${rows.map(row => `<tr><td><strong>${esc(userName(row.user_id))}</strong></td><td>${esc(nodeName(row.node_id))}</td><td class="align-right numeric">${esc(formatBytes(row.up))}</td><td class="align-right numeric">${esc(formatBytes(row.down))}</td><td class="align-right numeric"><strong>${esc(formatBytes(row.up + row.down))}</strong></td></tr>`).join('')}</tbody></table></div>` : empty('usage', 'noUsage', 'noUsageDetail')}</section><div class="notice">${icon('clock')}<span>${label('usageNote')}</span></div>`;
}

function field(name, key, value = '', options = {}) {
  return `<label class="field${options.wide ? ' field-wide' : ''}"><span>${label(key)}${options.required ? '<b aria-hidden="true"> *</b>' : ''}</span><input name="${name}" type="${options.type || 'text'}" value="${esc(value)}" ${options.required ? 'required' : ''} ${options.min !== undefined ? `min="${options.min}"` : ''} ${options.max !== undefined ? `max="${options.max}"` : ''} ${options.step ? `step="${options.step}"` : ''} ${options.placeholder ? `placeholder="${esc(options.placeholder)}"` : ''} ${options.maxlength ? `maxlength="${options.maxlength}"` : ''} autocomplete="off">${options.help ? `<small>${label(options.help)}</small>` : ''}</label>`;
}
function dialogShell(title, body, footer, wide = false) {
  dialog.className = wide ? 'dialog-wide' : '';
  dialog.innerHTML = `<div class="dialog-heading"><h2 id="dialog-title">${esc(title)}</h2><button type="button" class="icon-button" data-action="close-dialog" aria-label="${label('close')}">${icon('close')}</button></div>${body}<div class="dialog-footer">${footer}</div>`;
  if (!dialog.open) dialog.showModal();
}
function closeDialog() { dialog.close(); dialog.innerHTML = ''; modalRender = null; }
function toLocalInput(value) { if (!value) return ''; const date = new Date(value); if (Number.isNaN(date.getTime())) return ''; return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16); }

function editUser(user) {
  modalRender = () => editUser(user);
  const editing = Boolean(user);
  const value = user || {enabled: true, grants: []};
  const body = `<form id="user-form" data-id="${esc(user?.id || '')}" class="dialog-body" novalidate><div class="form-grid">${field('name', 'name', value.name, {required: true, maxlength: 100, wide: true, placeholder: t('userNamePlaceholder')})}${field('quota', 'quotaGiB', (value.quota_bytes || 0) / 1024 ** 3, {type: 'number', min: 0, step: 'any', help: 'quotaHelp'})}${field('expires_at', 'expiresAt', toLocalInput(value.expires_at), {type: 'datetime-local', help: 'expiresHelp'})}<label class="checkbox-label field-wide"><input name="enabled" type="checkbox"${value.enabled ? ' checked' : ''}><span>${label('userEnabled')}</span></label></div><fieldset class="grant-fieldset"><legend>${label('access')}</legend><p>${label('grantHelp')}</p><div class="grant-options">${state.nodes.length ? state.nodes.map(node => `<label class="grant-option"><input type="checkbox" name="grants" value="${esc(node.id)}"${value.grants?.includes(node.id) ? ' checked' : ''}><span><strong>${esc(node.name)}</strong><small>${esc(node.proxy_host)}</small></span>${badge(health(node))}</label>`).join('') : `<div class="inline-empty">${label('grantEmpty')}</div>`}</div></fieldset><div class="form-error" role="alert" hidden></div></form>${editing ? `<div class="dialog-secondary-actions">${button('rotate-proxy', 'rotateProxy', user.id, 'button button-small button-secondary')}${button('delete-user', 'remove', user.id, 'button button-small button-danger-ghost')}</div>` : ''}`;
  dialogShell(t(editing ? 'editUser' : 'addUser'), body, `${button('close-dialog', 'cancel')}<button type="submit" form="user-form" class="button button-primary">${label(editing ? 'save' : 'create')}</button>`, true);
}

function editNode(node) {
  modalRender = () => editNode(node);
  const editing = Boolean(node);
  const value = node || {ssh_port: 22, ssh_user: 'naive-manager', proxy_port: 443, protocol: 'https'};
  const body = `<form id="node-form" data-id="${esc(node?.id || '')}" class="dialog-body" novalidate><div class="notice compact-notice">${icon('shield')}<span>${label('bootstrapNote')}</span></div><div class="form-grid">${field('name', 'name', value.name, {required: true, wide: true, maxlength: 100, placeholder: t('nodeNamePlaceholder')})}<h3 class="form-section-title">${label('sshSettings')}</h3>${field('host', 'host', value.host, {required: true, maxlength: 253, placeholder: 'node.example.com'})}${field('ssh_port', 'sshPort', value.ssh_port, {required: true, type: 'number', min: 1, max: 65535, step: '1'})}${field('ssh_user', 'sshUser', value.ssh_user, {required: true, maxlength: 64})}${field('key_ref', 'keyRef', value.key_ref, {required: true, maxlength: 100, help: 'keyHelp', placeholder: 'node-01'})}${field('host_fingerprint', 'hostFingerprint', value.host_fingerprint, {required: true, wide: true, maxlength: 100, help: 'fingerprintHelp', placeholder: 'SHA256:…'})}<h3 class="form-section-title">${label('proxySettings')}</h3>${field('proxy_host', 'proxyHost', value.proxy_host, {required: true, maxlength: 253, placeholder: 'proxy.example.com'})}${field('proxy_port', 'proxyPort', value.proxy_port, {required: true, type: 'number', min: 1, max: 65535, step: '1'})}<label class="field"><span>${label('protocol')}</span><select name="protocol"><option value="https"${value.protocol === 'https' ? ' selected' : ''}>${label('https')}</option><option value="quic"${value.protocol === 'quic' ? ' selected' : ''}>${label('quic')}</option></select></label></div><div class="form-error" role="alert" hidden></div></form>${editing ? `<div class="dialog-secondary-actions">${button('restart', 'restart', node.id, 'button button-small button-secondary')}${button('delete-node', 'remove', node.id, 'button button-small button-danger-ghost')}</div>` : ''}`;
  dialogShell(t(editing ? 'editNode' : 'addNode'), body, `${button('close-dialog', 'cancel')}<button type="submit" form="node-form" class="button button-primary">${label(editing ? 'save' : 'create')}</button>`, true);
}

function showSubscription(user) {
  modalRender = () => showSubscription(state.users.find(item => item.id === user.id) || user);
  dialogShell(t('subscriptionTitle'), `<div class="dialog-body"><div class="subscription-person">${icon('users')}<strong>${esc(user.name)}</strong>${badge(userStatus(user))}</div><p class="dialog-description">${label('subscriptionHelp')}</p><label class="field"><span>${label('subscription')}</span><input class="subscription-url" readonly value="${esc(subscriptionURL(user))}" data-subscription-url></label><div class="subscription-actions">${button('copy-subscription', 'copyLink', user.id, 'button button-primary')}<a class="button button-secondary" href="${esc(subscriptionURL(user))}" target="_blank" rel="noopener noreferrer">${label('openSubscription')}</a></div></div><div class="dialog-secondary-actions">${button('rotate-subscription', 'rotateSubscription', user.id, 'button button-small button-danger-ghost')}</div>`, button('close-dialog', 'close'));
}

function confirmAction(title, description, action, id, key, danger = true) {
  modalRender = null;
  dialogShell(t(title), `<div class="dialog-body"><div class="confirm-symbol${danger ? ' confirm-danger' : ''}">${icon('alert')}</div><p class="dialog-description">${esc(description)}</p><div class="form-error" role="alert" hidden></div></div>`, `${button('close-dialog', 'cancel')}${button(action, key, id, `button ${danger ? 'button-danger' : 'button-primary'}`)}`);
}

async function refresh(silent = false) {
  if (refreshing || !authenticated) return;
  refreshing = true;
  if (!silent && !dialog.open) render();
  try {
    const result = await api('/api/state');
    state = {...result, users: result.users || [], nodes: result.nodes || [], usage: result.usage || []};
    lastRefresh = result.server_time || new Date().toISOString();
  } catch (error) { notify(error.message, true); }
  finally { refreshing = false; if (!dialog.open && !document.activeElement?.matches('[data-search]')) render(); }
}

function formError(form, message) { const box = form.querySelector('.form-error') || dialog.querySelector('.form-error'); if (box) { box.textContent = message; box.hidden = false; } else notify(message, true); }
function validateForm(form) {
  for (const input of form.querySelectorAll('input')) {
    input.setCustomValidity('');
    let key = '';
    if (input.validity.valueMissing || (input.required && input.type === 'text' && !input.value.trim())) key = 'required';
    else if (input.validity.rangeUnderflow || input.validity.rangeOverflow) key = input.name.includes('port') ? 'invalidPort' : input.name === 'quota' ? 'invalidQuota' : 'invalidValue';
    else if (input.validity.tooLong) key = 'fieldTooLong';
    else if (!input.validity.valid) key = 'invalidValue';
    if (key) { input.setCustomValidity(t(key)); input.reportValidity(); return false; }
  }
  return true;
}
function setBusy(busy) {
  requestInProgress = busy;
  for (const buttonElement of dialog.querySelectorAll('button')) buttonElement.disabled = busy;
  dialog.setAttribute('aria-busy', String(busy));
}
async function mutate(path, method, body, successKey, after) {
  if (requestInProgress) return;
  setBusy(true);
  try {
    await api(path, {method, body});
    closeDialog();
    await refresh(true);
    notify(t(successKey));
    if (after) after();
  } catch (error) { formError(dialog, error.message); }
  finally { setBusy(false); }
}

document.addEventListener('submit', async event => {
  const form = event.target;
  if (!['login-form', 'user-form', 'node-form'].includes(form.id)) return;
  event.preventDefault();
  if (!validateForm(form) || requestInProgress) return;
  const data = new FormData(form);
  if (form.id === 'login-form') {
    const submit = form.querySelector('button');
    submit.disabled = true;
    submit.textContent = t('loggingIn');
    try {
      const result = await api('/api/login', {method: 'POST', body: {password: data.get('password')}});
      csrf = result.csrf;
      authenticated = true;
      await refresh();
    } catch (error) { formError(form, error.message); submit.disabled = false; submit.textContent = t('login'); }
    return;
  }
  const id = form.dataset.id;
  if (form.id === 'user-form') {
    const expires = data.get('expires_at');
    const quota = Number(data.get('quota')) * 1024 ** 3;
    if (!Number.isSafeInteger(Math.round(quota)) || quota < 0) { formError(form, t('invalidQuota')); return; }
    const body = {name: data.get('name').trim(), enabled: data.has('enabled'), expires_at: expires ? new Date(expires).toISOString() : '', quota_bytes: Math.round(quota), grants: data.getAll('grants')};
    await mutate(`/api/users${id ? `/${encodeURIComponent(id)}` : ''}`, id ? 'PUT' : 'POST', body, 'saved');
  } else {
    const body = Object.fromEntries(data);
    for (const name of ['name', 'host', 'ssh_user', 'key_ref', 'host_fingerprint', 'proxy_host']) body[name] = body[name].trim();
    body.ssh_port = Number(body.ssh_port);
    body.proxy_port = Number(body.proxy_port);
    await mutate(`/api/nodes${id ? `/${encodeURIComponent(id)}` : ''}`, id ? 'PUT' : 'POST', body, 'saved');
  }
});

document.addEventListener('click', async event => {
  const element = event.target.closest('[data-action]');
  if (!element || element.disabled || requestInProgress) return;
  const action = element.dataset.action;
  const id = element.dataset.id;
  const user = state.users.find(item => item.id === id);
  const node = state.nodes.find(item => item.id === id);
  if (action === 'close-dialog') { closeDialog(); return; }
  if (action === 'refresh') { await refresh(); return; }
  if (action === 'add-user' || action === 'edit-user') { editUser(user); return; }
  if (action === 'add-node' || action === 'edit-node') { editNode(node); return; }
  if (action === 'subscription' && user) { showSubscription(user); return; }
  if (action === 'copy-subscription' && user) {
    try { await navigator.clipboard.writeText(subscriptionURL(user)); notify(t('copied')); }
    catch { dialog.querySelector('[data-subscription-url]')?.select(); notify(t('copyFailed'), true); }
    return;
  }
  if (action === 'logout') {
    try { await api('/api/logout', {method: 'POST'}); authenticated = false; csrf = ''; state = {users: [], nodes: [], usage: []}; closeDialog(); render(); }
    catch (error) { notify(error.message, true); }
    return;
  }
  if (action === 'delete-user') { confirmAction('deleteUserTitle', t('deleteUserDetail'), 'confirm-delete-user', id, 'remove'); return; }
  if (action === 'delete-node') { confirmAction('deleteNodeTitle', t('deleteNodeDetail'), 'confirm-delete-node', id, 'remove'); return; }
  if (action === 'rotate-proxy') { confirmAction('rotateProxyTitle', t('rotateProxyDetail'), 'confirm-rotate-proxy', id, 'rotate'); return; }
  if (action === 'rotate-subscription') { confirmAction('rotateSubscriptionTitle', t('rotateSubscriptionDetail'), 'confirm-rotate-subscription', id, 'rotate'); return; }
  if (action === 'restart' && node) { confirmAction('restartTitle', t('restartDetail', {name: node.name}), 'confirm-restart', id, 'restart'); return; }
  if (action === 'confirm-delete-user') { await mutate(`/api/users/${encodeURIComponent(id)}`, 'DELETE', undefined, 'deleted'); return; }
  if (action === 'confirm-delete-node') { await mutate(`/api/nodes/${encodeURIComponent(id)}`, 'DELETE', undefined, 'deleted'); return; }
  if (action.startsWith('confirm-rotate-')) { const kind = action.endsWith('proxy') ? 'proxy' : 'subscription'; await mutate(`/api/users/${encodeURIComponent(id)}/rotate`, 'POST', {kind}, kind === 'proxy' ? 'credentialsRotated' : 'subscriptionRotated', kind === 'subscription' ? () => { const updated = state.users.find(item => item.id === id); if (updated) showSubscription(updated); } : undefined); return; }
  if (['sync', 'collect', 'confirm-restart'].includes(action)) {
    const operation = action === 'confirm-restart' ? 'restart' : action;
    element.disabled = true;
    await mutate(`/api/nodes/${encodeURIComponent(id)}/${operation}`, 'POST', {}, `${operation}Complete`);
    element.disabled = false;
  }
});

document.addEventListener('change', event => {
  if (event.target.matches('[data-language]')) { setLocale(event.target.value); render(); if (modalRender) modalRender(); }
  if (event.target.matches('[data-usage-user]')) { usageUser = event.target.value; render(); }
  if (event.target.matches('[data-usage-node]')) { usageNode = event.target.value; render(); }
  if (event.target.matches('[data-usage-month]')) { usageMonth = event.target.value; render(); }
});
document.addEventListener('input', event => {
  if (event.target instanceof HTMLInputElement) event.target.setCustomValidity('');
  if (event.target.matches('[data-search]')) {
    search = event.target.value;
    const selection = event.target.selectionStart;
    render();
    const input = app.querySelector('[data-search]');
    input?.focus();
    try { input?.setSelectionRange(selection, selection); } catch { /* Search inputs vary by browser. */ }
  }
});
window.addEventListener('hashchange', () => { const next = location.hash.slice(1); if (tabs.includes(next)) { currentTab = next; search = ''; render(); } });
dialog.addEventListener('cancel', event => { if (requestInProgress) event.preventDefault(); else modalRender = null; });
dialog.addEventListener('click', event => { if (event.target === dialog && !requestInProgress) { const rect = dialog.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) closeDialog(); } });

async function start() {
  app.innerHTML = `<div class="initial-loading"><span class="brand-symbol">N<span></span></span><p>${label('loading')}</p></div>`;
  try {
    const session = await api('/api/session');
    authenticated = Boolean(session.authenticated);
    csrf = session.csrf || '';
    if (authenticated) await refresh(); else render();
  } catch (error) { render(); notify(error.message, true); }
}
setInterval(() => { if (authenticated && !dialog.open && !requestInProgress && !document.hidden) refresh(true); }, 60000);
start();
