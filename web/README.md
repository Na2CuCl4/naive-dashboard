# Dashboard browser application

The frontend uses native ES modules and local assets embedded through `web.FS`.
There is no frontend build step and no runtime CDN dependency. Both `zh-CN` and
`en` are maintained in `locales/`; the selected language persists in localStorage.
API failures use stable codes and are translated in the browser. Credentials and
subscription tokens are never kept in browser localStorage.

Run the dependency-free resource and CSP checks:

```sh
node --test web/i18n_test.mjs
```

`browser_test.mjs` is an optional real-browser integration test. It creates users
and nodes, so use a **disposable local dashboard database** with insecure cookies
enabled only on its local HTTP test address. Nodes with historical grants remain
after the test so pending revocations and final accounting cannot be lost.

Install Playwright outside the repository, then run against that test server:

```sh
npm install --prefix /tmp/naive-ui playwright@1.58.2
/tmp/naive-ui/node_modules/.bin/playwright install chromium
PLAYWRIGHT_MODULE=/tmp/naive-ui/node_modules/playwright/index.mjs \
  DASHBOARD_TEST_URL=http://127.0.0.1:18082 \
  DASHBOARD_TEST_PASSWORD='your-local-test-password' \
  DASHBOARD_TEST_SCREENSHOTS=/tmp/naive-ui/screenshots \
  node web/browser_test.mjs
```

Chromium requires its usual OS libraries and fonts. The test covers login,
language persistence, user/node creation, grants, native subscriptions, proxy and
subscription credential rotations, user disabling/deletion, retained historical
nodes, deletion of unused nodes, mobile layout/logout, and strict CSP. Screenshots
are optional and written only when `DASHBOARD_TEST_SCREENSHOTS` is set.
