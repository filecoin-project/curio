# Curio UI Visual Tests

Dev-only Playwright screenshot tests for the admin UI. Does **not** build or transform shipped assets in `web/static/`.

## Prerequisites

- Node.js 18+

## Setup

```bash
cd web/visual-test
npm install
npx playwright install chromium
```

## Run tests

```bash
npm test
```

Starts a minimal static server on port 8765, visits pages with `?mock=1` (fake RPC data), and compares screenshots to baselines.

## Update baselines

After intentional visual changes:

```bash
npm run test:update
```

Review diff images in `test-results/` before committing updated snapshots in `tests/visual.spec.js-snapshots/`.

## Pages covered

- `/debug/gallery/?mock=1` — style guide and token swatches
- `/?mock=1` — dashboard
- `/pages/alerts/?mock=1` — alerts management
- `/pages/task/?mock=1` — task details shell

## Mock mode

Append `?mock=1` to any page URL to render with fixture data (no cluster required). Persists in `sessionStorage` for in-app navigation.

Disable: `?mock=0`

## CI

Set `CI=1` to disable reuse of an existing local server. Baselines are checked in per viewport project (`desktop` 1440px, `wide` 2400px).
