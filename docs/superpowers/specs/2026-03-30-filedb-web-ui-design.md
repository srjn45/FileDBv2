# FileDB Web UI — Design Spec

**Date:** 2026-03-30
**Status:** Approved

---

## Overview

A browser-based admin UI for FileDB v2, comparable to phpMyAdmin for MySQL or ElasticVue for Elasticsearch. It lives at `clients/web/` as a standalone npm project (React + TypeScript + Vite) that talks to the FileDB REST gateway at `:8080`.

### Goals
- Browse and manage all collections
- Full CRUD on records with filtering, ordering, and pagination
- Manage secondary indexes per collection
- View collection stats
- Live change feed via the Watch streaming RPC
- Zero server-side changes required — uses the existing REST gateway

### Out of scope (v1)
- Transaction UI (BeginTx / CommitTx / RollbackTx)
- Prometheus metrics view
- Embedding into the Go binary

---

## Tech Stack

| Concern | Choice | Reason |
|---|---|---|
| Framework | React 18 + TypeScript | Best ecosystem for data-heavy admin UIs |
| Build tool | Vite | Fast dev server + hot reload |
| HTTP client | Fetch API wrapping existing `clients/js` types | Reuse proto-generated types from `clients/js` |
| Component library | shadcn/ui (Radix + Tailwind) | Dark-theme ready, accessible, unstyled base |
| State | React context + `useState` / `useEffect` | No global state manager needed at this scope |
| Streaming | `fetch` with `ReadableStream` (SSE-style) for Watch | REST gateway streams Watch responses |
| Persistence | `localStorage` | Connection settings (URL + API key) |

---

## Project Layout

```
clients/web/
├── index.html
├── package.json
├── vite.config.ts
├── tsconfig.json
├── src/
│   ├── main.tsx              # React root
│   ├── App.tsx               # App shell: top bar + sidebar + content area
│   ├── api/
│   │   └── client.ts         # Thin wrapper around fetch → REST gateway
│   ├── components/
│   │   ├── Sidebar.tsx       # Collection list + "New Collection" button
│   │   ├── CollectionView.tsx # Tab router: Browse / Indexes / Stats / Watch
│   │   ├── BrowseTab.tsx     # Filter bar + records table + pagination
│   │   ├── IndexesTab.tsx    # List indexes, ensure/drop
│   │   ├── StatsTab.tsx      # 4 stat cards, auto-refresh
│   │   ├── WatchTab.tsx      # Live event log
│   │   ├── RecordModal.tsx   # Insert / edit modal (JSON ↔ form toggle)
│   │   ├── FilterBar.tsx     # Field / op / value rows + AND/OR
│   │   ├── SettingsPanel.tsx # URL + API key form
│   │   └── Toast.tsx         # Success / error / info toasts
│   └── hooks/
│       ├── useCollections.ts # Fetch + refresh collection list
│       ├── useRecords.ts     # Find with filter/order/limit/offset
│       └── useWatch.ts       # ReadableStream-based Watch subscription
```

---

## Layout

```
┌─────────────────────────────────────────────────────────┐
│ ⬡ FileDB  │  localhost:8080  ● connected        ⚙       │  ← Top bar
├──────────────┬──────────────────────────────────────────┤
│ COLLECTIONS  │  Browse  │ Indexes │ Stats │ ⚡ Watch     │  ← Tab bar
│              ├──────────────────────────────────────────┤
│ ● users  142 │                                          │
│   products 38│         Tab content area                 │
│   orders 901 │                                          │
│              │                                          │
│  + New       │                                          │
└──────────────┴──────────────────────────────────────────┘
```

- **Top bar**: app logo, server URL, connection status badge, settings gear
- **Sidebar**: collection list with record counts, active collection highlighted, "+ New Collection" at bottom
- **Tab bar**: per-collection tabs — Browse / Indexes / Stats / Watch — plus "Drop Collection" action on the right

---

## Connection & Settings

- On first load (or when no settings in `localStorage`), show a centered **Connect** form: Server URL + API Key
- Settings saved to `localStorage` on submit; loaded on mount
- **Settings panel** (⚙ top-right): same URL + API key form, "Save & Reconnect" button
- All API calls send `x-api-key` header
- Connection status shown in top bar: green `● connected` or red `● disconnected`

---

## Browse Tab

### Filter Bar
- One or more filter rows: **field** (text input) | **op** (dropdown: eq / neq / gt / gte / lt / lte / contains / regex) | **value** (text input)
- "+ AND" and "+ OR" buttons add additional rows
- **Order by**: field name + asc/desc
- **Limit**: numeric input (default 20)
- **Run** executes the query; **Clear** resets all rows
- Maps to `POST /v1/{collection}/records/find` with composed `Filter` object

### Records Table
- Columns derived dynamically from the union of keys across returned records
- Always-present columns: `ID`, `created_at`, `modified_at`, `actions`
- Nested JSON objects rendered as collapsed `{…}` — click to expand inline
- Per-row actions: ✏ Edit (opens RecordModal pre-filled), ✕ Delete (confirm toast)
- Clicking the ID opens a full-screen record detail view
- Pagination: offset-based, 20 records per page, prev/next + page number buttons, total count shown

### Insert / Edit Record Modal
- Triggered by "+ Insert" button (new) or ✏ icon (edit)
- Two modes toggled with a button:
  - **JSON mode**: syntax-highlighted textarea, validates JSON on submit
  - **Form mode**: key-value rows, "+ Add field" to add more, ✕ to remove
- On submit: `POST /v1/{collection}/records` (insert) or `PUT /v1/{collection}/records/{id}` (update)

---

## Indexes Tab

- Lists all secondary indexes from `GET /v1/{collection}/indexes`
- Each index shown as a row with the field name and a **Drop** button (`DELETE /v1/{collection}/indexes/{field}`)
- "Ensure Index" input + button at the top: field name → `POST /v1/{collection}/indexes`
- Info note: "Indexed fields accelerate eq-filter queries from O(n) → O(1)"

---

## Stats Tab

Four stat cards from `GET /v1/{collection}/stats`:

| Card | Field | Notes |
|---|---|---|
| Records | `record_count` | Plain number |
| Segments | `segment_count` | Plain number |
| Dirty Entries | `dirty_entries` | Number + percentage of total + progress bar (amber when >20%) |
| Size on Disk | `size_bytes` | Human-readable (KB / MB / GB) |

- Auto-refreshes every 30 seconds
- Manual "↻ Refresh" button
- Last-refreshed timestamp shown

---

## Watch Tab

- Connects to `POST /v1/{collection}/watch` via `ReadableStream` (the REST gateway streams `Watch` RPC responses as newline-delimited JSON)
- **Start watching** on tab activation; **Stop** button disconnects
- Event log (newest at top, max 200 events in memory):
  - Timestamp | Op badge (INSERT green / UPDATE blue / DELETE red) | ID | Record data (collapsed JSON)
- **Clear** button empties the log
- `● watching` animated badge while connected
- Optional: filter input to show only events matching a field value

---

## Global Interactions

### Create Collection
- "+ New Collection" in sidebar → modal with collection name input
- Validates: non-empty, alphanumeric + underscores only
- On submit: `POST /v1/collections`, then refreshes sidebar

### Drop Collection
- "Drop Collection" in tab bar → confirmation modal
- User must type the collection name to enable the Drop button
- On confirm: `DELETE /v1/collections/{name}`, then navigates to next collection in list

### Toast Notifications
- Bottom-right stack, max 3 visible at once
- Auto-dismiss after 4 seconds
- Types: success (green), info (blue), error (red)
- Shown for: insert, update, delete, index create/drop, collection create/drop, connection errors

---

## API Client (`src/api/client.ts`)

Thin wrapper over `fetch`:

```typescript
class FileDBClient {
  constructor(baseUrl: string, apiKey: string)

  // Collections
  listCollections(): Promise<string[]>
  createCollection(name: string): Promise<void>
  dropCollection(name: string): Promise<void>

  // Records
  insert(collection: string, data: object): Promise<{ id: number }>
  findById(collection: string, id: number): Promise<Record>
  find(collection: string, req: FindRequest): Promise<Record[]>
  update(collection: string, id: number, data: object): Promise<void>
  delete(collection: string, id: number): Promise<void>

  // Indexes
  listIndexes(collection: string): Promise<string[]>
  ensureIndex(collection: string, field: string): Promise<void>
  dropIndex(collection: string, field: string): Promise<void>

  // Stats
  collectionStats(collection: string): Promise<CollectionStats>

  // Watch (streaming)
  watch(collection: string, onEvent: (e: WatchEvent) => void): () => void  // returns unsubscribe fn
}
```

---

## CORS

The REST gateway at `:8080` needs `Access-Control-Allow-Origin: *` (or the UI's origin) for browser requests. This requires adding a CORS middleware to `server/rest.go`. The UI will document this as a prerequisite in its README.

---

## Error Handling

- All API errors surface as toast notifications with the HTTP status + message
- 401 Unauthorized → also shows "Check your API key in Settings"
- Network errors (server unreachable) → top bar switches to `● disconnected`, toast shown
- JSON parse errors in the modal → inline validation message, no submit

---

## Development Setup

```bash
cd clients/web
npm install
npm run dev        # starts on http://localhost:5173, proxies /v1 → localhost:8080
npm run build      # produces dist/ for deployment
```

Vite dev server proxies `/v1` to avoid CORS issues in development. Production build requires CORS to be enabled on the server.

---

## Documentation Updates Required

Per project conventions, the following must be updated when this feature lands:

- `docs/getting-started.md` — add "Web UI" section with setup instructions
- `docs/architecture.md` — mention the web client and CORS requirement
- `README.md` — add web UI to key properties list
- `ROADMAP.md` — mark web UI item as done
