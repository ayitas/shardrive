# Shardrive Frontend

Phase 1 SvelteKit frontend scaffold. It uses strict TypeScript and keeps API
transport separate from UI components.

## Development

Requirements: Node.js 20+ and npm 10+.

```sh
npm install
npm run check
npm run dev -- --open
```

Set `PUBLIC_API_BASE_URL` when the API is not served from the same origin. The
client always uses cookie credentials; it never stores auth tokens or provider
credentials in browser storage.

## Firefox E2E

Install the Playwright Firefox and Chromium browsers once, then run the Phase 1
gate against an already-running Docker Compose stack:

```sh
npx playwright install firefox
npx playwright install chromium
E2E_EMAIL=you@example.test \
E2E_PASSWORD='use-a-local-password' \
npm run test:e2e
```

The test runs in both Firefox and Chromium. It creates its fixture in memory,
verifies the downloaded SHA-256, and waits for the asynchronous delete worker
to release quota. Credentials are read only from the environment and are not
stored in the repository.

## Proton browser gate

The opt-in one-account Proton gate runs against the Proton Compose profile and
the already-imported encrypted session. From the repository root:

```sh
E2E_EMAIL=you@example.test \
E2E_PASSWORD='use-a-local-password' \
E2E_PROTON_ACCOUNT_NAME='Personal Proton' \
E2E_PROTON_ACCOUNT_REF=account-1 \
make proton-e2e
```

The gate connects and refreshes the account in Firefox; it does not upload or
delete remote data. Active uploads are server-visible through `GET
/api/v1/uploads`, so a browser profile can resume or cancel an interrupted
session after selecting the matching original file.
