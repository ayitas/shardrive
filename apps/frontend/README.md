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
