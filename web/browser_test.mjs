// Run only against a disposable local dashboard. Playwright is an optional test tool;
// no package install or JavaScript build is needed to run the dashboard itself.
// PLAYWRIGHT_MODULE=/tmp/naive-ui/node_modules/playwright/index.mjs
// DASHBOARD_TEST_URL=http://127.0.0.1:18082 DASHBOARD_TEST_PASSWORD=... node web/browser_test.mjs
import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';

const base = process.env.DASHBOARD_TEST_URL;
const password = process.env.DASHBOARD_TEST_PASSWORD;
if (!base || !password || !['127.0.0.1', 'localhost', '[::1]'].includes(new URL(base).hostname)) {
  throw new Error('Set DASHBOARD_TEST_URL to a disposable localhost dashboard and provide DASHBOARD_TEST_PASSWORD.');
}
const {chromium} = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless: true});
const context = await browser.newContext({viewport: {width: 1440, height: 1040}});
const page = await context.newPage();
const browserErrors = [];
page.on('pageerror', error => browserErrors.push(error.message));
page.on('console', message => { if (message.type() === 'error' && /Content Security Policy|Refused to/.test(message.text())) browserErrors.push(message.text()); });
const suffix = Date.now().toString(36);
const userName = `Browser user ${suffix}`;
const nodeName = `Browser node ${suffix}`;
const snapshots = process.env.DASHBOARD_TEST_SCREENSHOTS;
const click = async action => page.locator(`[data-action="${action}"]`).first().click();
const dialogClosed = () => page.locator('#editor').waitFor({state: 'hidden'});
const chooseTab = async tab => { await page.locator(`.sidebar nav a[href="#${tab}"]`).click(); await page.locator('.page-heading h1').waitFor(); };
const userRow = () => page.locator('tr').filter({hasText: userName});
const nodeCard = () => page.locator('.node-card').filter({hasText: nodeName});

try {
  await page.goto(base);
  await page.locator('#login-form').waitFor();
  assert.equal(await page.locator('html').getAttribute('lang'), 'zh-CN');
  await page.locator('[data-language]').selectOption('en');
  await page.locator('input[name="password"]').fill(password);
  await page.locator('#login-form button[type="submit"]').click();
  await page.locator('.dashboard-shell').waitFor();
  await page.reload();
  await page.locator('.dashboard-shell').waitFor();
  assert.equal(await page.locator('html').getAttribute('lang'), 'en');

  await chooseTab('nodes');
  await click('add-node');
  await page.locator('#node-form input[name="name"]').fill(nodeName);
  await page.locator('#node-form input[name="host"]').fill('127.0.0.1');
  await page.locator('#node-form input[name="ssh_port"]').fill('22222');
  await page.locator('#node-form input[name="key_ref"]').fill(`browser-key-${suffix}`);
  await page.locator('#node-form input[name="host_fingerprint"]').fill(`SHA256:${Buffer.alloc(32).toString('base64url')}`);
  await page.locator('#node-form input[name="proxy_host"]').fill('test.example.com');
  await page.locator('button[form="node-form"]').click();
  await dialogClosed();
  await nodeCard().waitFor();
  const nodeID = await nodeCard().locator('[data-action="edit-node"]').getAttribute('data-id');

  await chooseTab('users');
  await click('add-user');
  await page.locator('#user-form input[name="name"]').fill(userName);
  await page.locator('#user-form input[name="quota"]').fill('10');
  await page.locator(`#user-form input[name="grants"][value="${nodeID}"]`).check();
  await page.locator('button[form="user-form"]').click();
  await dialogClosed();
  await userRow().waitFor();
  await userRow().locator('[data-action="subscription"]').click();
  const previousSubscription = await page.locator('[data-subscription-url]').inputValue();
  const subscription = await context.request.get(previousSubscription);
  assert.equal(subscription.status(), 200);
  assert.match(await subscription.text(), /naive\+https:\/\//);
  await click('rotate-subscription');
  await click('confirm-rotate-subscription');
  await page.locator('[data-subscription-url]').waitFor();
  const replacementSubscription = await page.locator('[data-subscription-url]').inputValue();
  assert.notEqual(previousSubscription, replacementSubscription);
  assert.equal((await context.request.get(previousSubscription)).status(), 404);
  await click('close-dialog');

  await userRow().locator('[data-action="edit-user"]').click();
  await click('rotate-proxy');
  await click('confirm-rotate-proxy');
  await dialogClosed();
  assert.equal((await context.request.get(replacementSubscription)).status(), 200);

  await chooseTab('overview');
  if (snapshots) { await mkdir(snapshots, {recursive: true}); await page.screenshot({path: `${snapshots}/dashboard-desktop.png`, fullPage: true}); }
  await page.locator('[data-language]').selectOption('zh-CN');
  assert.equal(await page.locator('.page-heading h1').innerText(), '集群运行概览');
  await page.reload();
  await page.locator('.dashboard-shell').waitFor();
  assert.equal(await page.locator('html').getAttribute('lang'), 'zh-CN');

  await page.setViewportSize({width: 390, height: 844});
  await chooseTab('nodes');
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'mobile page must not overflow horizontally');
  if (snapshots) await page.screenshot({path: `${snapshots}/dashboard-mobile.png`, fullPage: true});
  await page.setViewportSize({width: 1440, height: 1040});
  await page.locator('[data-language]').selectOption('en');
  await nodeCard().locator('[data-action="edit-node"]').click();
  await click('delete-node');
  await click('confirm-delete-node');
  await page.locator('#editor .form-error:not([hidden])').waitFor();
  await click('close-dialog');

  await chooseTab('users');
  await userRow().locator('[data-action="edit-user"]').click();
  await page.locator('#user-form input[name="enabled"]').uncheck();
  await page.locator('button[form="user-form"]').click();
  await dialogClosed();
  assert.equal(await userRow().locator('.badge').innerText(), 'Disabled');
  assert.equal((await context.request.get(replacementSubscription)).status(), 404);
  await userRow().locator('[data-action="edit-user"]').click();
  await click('delete-user');
  await click('confirm-delete-user');
  await dialogClosed();
  assert.equal(await userRow().count(), 0);
  await chooseTab('nodes');
  await nodeCard().locator('[data-action="edit-node"]').click();
  await click('delete-node');
  await click('confirm-delete-node');
  await page.locator('#editor .form-error:not([hidden])').waitFor();
  await click('close-dialog');
  assert.equal(await nodeCard().count(), 1, 'historical node remains for revocation and final usage settlement');

  await click('add-node');
  const unusedName = `Unused node ${suffix}`;
  await page.locator('#node-form input[name="name"]').fill(unusedName);
  await page.locator('#node-form input[name="host"]').fill('127.0.0.1');
  await page.locator('#node-form input[name="key_ref"]').fill(`unused-key-${suffix}`);
  await page.locator('#node-form input[name="host_fingerprint"]').fill(`SHA256:${Buffer.alloc(32).toString('base64url')}`);
  await page.locator('#node-form input[name="proxy_host"]').fill('unused.example.com');
  await page.locator('button[form="node-form"]').click();
  await dialogClosed();
  const unusedCard = page.locator('.node-card').filter({hasText: unusedName});
  await unusedCard.locator('[data-action="edit-node"]').click();
  await click('delete-node');
  await click('confirm-delete-node');
  await dialogClosed();
  assert.equal(await unusedCard.count(), 0);
  await page.setViewportSize({width: 390, height: 844});
  await page.locator('.topbar [data-action="logout"]').click();
  await page.locator('#login-form').waitFor();
  assert.deepEqual(browserErrors, [], 'no JavaScript or CSP errors');
  console.log('Browser checks passed: login, persisted bilingual UI, node/user creation, grants, subscriptions, both rotations, guarded node deletion, disable/delete, mobile layout, mobile logout, and strict CSP.');
} finally {
  await browser.close();
}
