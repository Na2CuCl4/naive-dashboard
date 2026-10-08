import zhCN from './locales/zh-CN.js';
import en from './locales/en.js';

export const resources = {'zh-CN': zhCN, en};
let language = 'zh-CN';
try { language = localStorage.getItem('naive.language') || language; } catch { /* Preference is optional. */ }
if (!resources[language]) language = 'zh-CN';

export function locale() { return language; }
export function setLocale(next) {
  language = resources[next] ? next : 'zh-CN';
  document.documentElement.lang = language;
  try { localStorage.setItem('naive.language', language); } catch { /* Preference is optional. */ }
}
export function t(key, values = {}) {
  let message = resources[language][key] || resources.en[key] || key;
  for (const [name, value] of Object.entries(values)) message = message.replaceAll(`{${name}}`, String(value));
  return message;
}
export function formatNumber(value) { return new Intl.NumberFormat(language).format(Number(value) || 0); }
export function formatBytes(value) {
  const bytes = Number(value) || 0;
  if (bytes < 1024) return `${formatNumber(bytes)} B`;
  const units = ['KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  const power = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length);
  return `${new Intl.NumberFormat(language, {maximumFractionDigits: 2}).format(bytes / 1024 ** power)} ${units[power - 1]}`;
}
export function formatDate(value) {
  if (!value) return t('never');
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return t('never');
  return new Intl.DateTimeFormat(language, {month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'}).format(date);
}
