import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import en from './locales/en.js';
import zhCN from './locales/zh-CN.js';
import {t, locale, setLocale, formatBytes, formatDate} from './i18n.js';

test('both locales cover the same keys and interpolation parameters', () => {
  assert.deepEqual(Object.keys(zhCN).sort(), Object.keys(en).sort());
  for (const key of Object.keys(en)) {
    assert.equal(typeof en[key], 'string');
    assert.ok(en[key].trim(), `English key ${key} is empty`);
    assert.ok(zhCN[key].trim(), `Chinese key ${key} is empty`);
    assert.deepEqual((en[key].match(/\{\w+\}/g) || []).sort(), (zhCN[key].match(/\{\w+\}/g) || []).sort(), key);
  }
});

test('all statically referenced application messages exist in both locales', async () => {
  const source = await readFile(new URL('./app.js', import.meta.url), 'utf8');
  for (const [, key] of source.matchAll(/\b(?:label|t)\('([^']+)'/g)) {
    assert.ok(key in en, `Missing English message: ${key}`);
    assert.ok(key in zhCN, `Missing Chinese message: ${key}`);
  }
  for (const page of ['overview', 'users', 'nodes', 'usage']) {
    for (const suffix of ['Title', 'Intro']) assert.ok(`${page}${suffix}` in en);
  }
});

test('language choice is constrained, persisted, and applied to document', () => {
  const saved = new Map();
  globalThis.document = {documentElement: {lang: ''}};
  globalThis.localStorage = {setItem: (key, value) => saved.set(key, value)};
  setLocale('en');
  assert.equal(locale(), 'en');
  assert.equal(document.documentElement.lang, 'en');
  assert.equal(saved.get('naive.language'), 'en');
  assert.equal(t('enabledUsers', {count: 3}), '3 enabled');
  assert.equal(formatBytes(1024), '1 KiB');
  assert.equal(formatBytes(1024 ** 3 * 2.5), '2.5 GiB');
  assert.equal(formatDate(''), 'Never');
  setLocale('fr');
  assert.equal(locale(), 'zh-CN');
  assert.equal(t('enabledUsers', {count: 3}), '3 个已启用');
  assert.equal(formatDate('invalid'), '暂无');
});

test('browser assets remain compatible with strict content security policy', async () => {
  const [html, js, css] = await Promise.all(['index.html', 'app.js', 'styles.css'].map(name => readFile(new URL(name, import.meta.url), 'utf8')));
  assert.doesNotMatch(html, /\son\w+\s*=/i);
  assert.doesNotMatch(html, /<style[\s>]|<script(?![^>]*src=)/i);
  assert.doesNotMatch(js, /style\s*=/i);
  assert.doesNotMatch(css, /@import/);
});
