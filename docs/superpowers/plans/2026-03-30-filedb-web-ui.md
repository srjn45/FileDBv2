# FileDB Web UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a browser-based admin UI at `clients/web/` (React + TypeScript + Vite + Tailwind CSS) that connects to the FileDB REST gateway and lets users browse collections, do full CRUD, manage indexes, view stats, and watch a live change feed.

**Architecture:** A standalone npm SPA that talks to the existing REST gateway at `:8080` via plain `fetch`. Two server-side prerequisites are required first: (1) a Watch HTTP annotation added to `proto/filedb.proto` so the REST gateway exposes `/v1/{collection}/watch`, and (2) a CORS middleware wrapping `server/rest.go` so browsers can make cross-origin requests. The UI uses React context for connection state and toasts, Tailwind CSS for styling (dark theme), and `localStorage` for persisting the server URL and API key.

**Tech Stack:** React 18, TypeScript 5, Vite 5, Tailwind CSS v3, Vitest (unit tests), no shadcn/ui CLI (plain Tailwind components for reliability in agentic execution)

---

## File Map

### Server changes (prerequisites)
- Modify: `proto/filedb.proto` — add HTTP annotation to `Watch` RPC
- Modify: `server/rest.go` — wrap handler with CORS middleware

### New files in `clients/web/`
```
clients/web/
├── index.html
├── package.json
├── vite.config.ts
├── tsconfig.json
├── tailwind.config.ts
├── postcss.config.cjs
├── src/
│   ├── main.tsx
│   ├── index.css                    # Tailwind directives + dark base
│   ├── App.tsx                      # Shell: topbar + sidebar + content
│   ├── api/
│   │   ├── types.ts                 # Re-export from clients/js types (DBRecord, WatchEvent, etc.)
│   │   └── client.ts                # REST-based FileDBClient (fetch, not gRPC)
│   ├── contexts/
│   │   ├── AppContext.tsx           # Connection state: url, apiKey, connected, client
│   │   └── ToastContext.tsx         # Toast stack: addToast, removeToast
│   ├── components/
│   │   ├── ConnectScreen.tsx        # Centered form shown when not connected
│   │   ├── SettingsPanel.tsx        # Slide-in panel from ⚙ gear icon
│   │   ├── Sidebar.tsx              # Collection list + New/Drop collection
│   │   ├── CollectionView.tsx       # Tab router: Browse / Indexes / Stats / Watch
│   │   ├── FilterBar.tsx            # Field/op/value rows, AND/OR, order, limit
│   │   ├── BrowseTab.tsx            # FilterBar + RecordsTable + pagination
│   │   ├── RecordModal.tsx          # Insert/edit: JSON ↔ form toggle
│   │   ├── IndexesTab.tsx           # List + ensure + drop indexes
│   │   ├── StatsTab.tsx             # 4 stat cards, auto-refresh
│   │   ├── WatchTab.tsx             # Live event log
│   │   └── Toast.tsx                # Toast stack renderer
│   └── hooks/
│       ├── useCollections.ts        # fetch + refresh collection list
│       ├── useRecords.ts            # find with filter/order/limit/offset
│       └── useWatch.ts              # ReadableStream Watch subscription
```

### Documentation
- Modify: `docs/getting-started.md`
- Modify: `docs/architecture.md`
- Modify: `README.md`
- Modify: `ROADMAP.md`

---

## Task 1: Add Watch REST annotation + CORS middleware

**Files:**
- Modify: `proto/filedb.proto`
- Modify: `server/rest.go`

The Watch RPC currently has no HTTP annotation, so the REST gateway doesn't expose it. We need to add one. We also need CORS so browsers can call `:8080` from the dev server at `:5173`.

- [ ] **Step 1: Add HTTP annotation to Watch RPC in proto**

In `proto/filedb.proto`, find the Watch RPC (around line 109) and add the HTTP annotation:

```protobuf
  rpc Watch(WatchRequest) returns (stream WatchEvent) {
    option (google.api.http) = {
      post: "/v1/{collection}/watch"
      body: "*"
    };
  }
```

- [ ] **Step 2: Regenerate gRPC stubs**

```bash
make proto
```

Expected: no errors, `internal/pb/proto/filedb.pb.gw.go` updated with Watch handler.

- [ ] **Step 3: Add CORS middleware to `server/rest.go`**

Replace the content of `server/rest.go` with:

```go
package server

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/srjn45/filedbv2/internal/pb/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// headerMatcher forwards x-api-key and all default grpc-gateway headers.
func headerMatcher(key string) (string, bool) {
	if strings.ToLower(key) == "x-api-key" {
		return "x-api-key", true
	}
	return runtime.DefaultHeaderMatcher(key)
}

// corsMiddleware adds permissive CORS headers so browser clients (e.g. the
// web UI dev server at :5173) can call the REST gateway at :8080.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, x-api-key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// NewRESTGateway returns an http.Handler that proxies requests to the gRPC
// server listening on grpcAddr via the grpc-gateway.
// creds controls how the gateway dials gRPC (pass insecure.NewCredentials() when TLS is off).
func NewRESTGateway(ctx context.Context, grpcAddr string, creds credentials.TransportCredentials) (http.Handler, error) {
	mux := runtime.NewServeMux(runtime.WithIncomingHeaderMatcher(headerMatcher))
	opts := []grpc.DialOption{grpc.WithTransportCredentials(creds)}

	if err := pb.RegisterFileDBHandlerFromEndpoint(ctx, mux, grpcAddr, opts); err != nil {
		return nil, err
	}
	return corsMiddleware(mux), nil
}

// NewRESTGatewayUnix returns an http.Handler that dials the gRPC server via a
// Unix domain socket. Unix sockets are always local, so insecure credentials
// are used regardless of the server's TLS setting.
func NewRESTGatewayUnix(ctx context.Context, socketPath string) (http.Handler, error) {
	mux := runtime.NewServeMux(runtime.WithIncomingHeaderMatcher(headerMatcher))
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}),
	}
	if err := pb.RegisterFileDBHandlerFromEndpoint(ctx, mux, "unix://"+socketPath, opts); err != nil {
		return nil, err
	}
	return corsMiddleware(mux), nil
}
```

- [ ] **Step 4: Run all server tests to verify nothing broke**

```bash
make test
```

Expected: all tests pass, race detector clean.

- [ ] **Step 5: Commit**

```bash
git add proto/filedb.proto server/rest.go internal/pb/proto/
git commit -m "feat(server): add Watch REST annotation and CORS middleware for web UI"
```

---

## Task 2: Scaffold `clients/web/`

**Files:**
- Create: `clients/web/package.json`
- Create: `clients/web/index.html`
- Create: `clients/web/vite.config.ts`
- Create: `clients/web/tsconfig.json`
- Create: `clients/web/tailwind.config.ts`
- Create: `clients/web/postcss.config.cjs`
- Create: `clients/web/src/main.tsx`
- Create: `clients/web/src/index.css`
- Create: `clients/web/src/App.tsx`

- [ ] **Step 1: Create `clients/web/package.json`**

```json
{
  "name": "filedb-web",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "test": "vitest run"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@types/react": "^18.3.1",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.4",
    "autoprefixer": "^10.4.20",
    "postcss": "^8.4.49",
    "tailwindcss": "^3.4.17",
    "typescript": "^5.7.2",
    "vite": "^5.4.11",
    "vitest": "^2.1.8"
  }
}
```

- [ ] **Step 2: Create `clients/web/index.html`**

```html
<!DOCTYPE html>
<html lang="en" class="dark">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>FileDB</title>
  </head>
  <body class="bg-gray-950 text-gray-100 min-h-screen">
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 3: Create `clients/web/vite.config.ts`**

```typescript
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/v1': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
  },
})
```

- [ ] **Step 4: Create `clients/web/tsconfig.json`**

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true
  },
  "include": ["src"]
}
```

- [ ] **Step 5: Create `clients/web/tailwind.config.ts`**

```typescript
import type { Config } from 'tailwindcss'

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        brand: '#6c8ebf',
      },
    },
  },
  plugins: [],
} satisfies Config
```

- [ ] **Step 6: Create `clients/web/postcss.config.cjs`**

```javascript
module.exports = {
  plugins: {
    tailwindcss: {},
    autoprefixer: {},
  },
}
```

- [ ] **Step 7: Create `clients/web/src/index.css`**

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

@layer base {
  body {
    @apply bg-gray-950 text-gray-100;
  }
  input, select, textarea {
    @apply bg-gray-800 text-gray-100 border border-gray-600 rounded px-2 py-1 text-sm outline-none focus:border-blue-500;
  }
}
```

- [ ] **Step 8: Create `clients/web/src/main.tsx`**

```tsx
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
```

- [ ] **Step 9: Create `clients/web/src/App.tsx` (placeholder)**

```tsx
export default function App() {
  return <div className="p-4 text-gray-100">FileDB UI — loading...</div>
}
```

- [ ] **Step 10: Install dependencies and verify dev server starts**

```bash
cd clients/web
npm install
npm run dev
```

Expected: Vite starts on `http://localhost:5173`, page shows "FileDB UI — loading..."

- [ ] **Step 11: Commit**

```bash
cd ../..
git add clients/web/
git commit -m "feat(web): scaffold React + Vite + Tailwind project"
```

---

## Task 3: API types and REST client

**Files:**
- Create: `clients/web/src/api/types.ts`
- Create: `clients/web/src/api/client.ts`
- Create: `clients/web/src/api/client.test.ts`

- [ ] **Step 1: Create `clients/web/src/api/types.ts`**

These mirror `clients/js/src/types.ts` but are rewritten for the REST context (no gRPC-specific types needed).

```typescript
/** A record returned from FileDB. id is a string because uint64 exceeds JS safe integer range. */
export interface DBRecord {
  id: string
  data: Record<string, unknown>
  date_added?: string
  date_modified?: string
}

/** An event from the Watch streaming endpoint. */
export interface WatchEvent {
  op: 'INSERTED' | 'UPDATED' | 'DELETED'
  collection: string
  record: DBRecord
  ts?: string
}

export type FilterOp = 'eq' | 'neq' | 'gt' | 'gte' | 'lt' | 'lte' | 'contains' | 'regex'

export interface FieldFilter {
  field: string
  op: FilterOp
  value: string
}

export interface AndFilter {
  and: Filter[]
}

export interface OrFilter {
  or: Filter[]
}

export type Filter = FieldFilter | AndFilter | OrFilter

export interface FindRequest {
  filter?: Filter
  limit?: number
  offset?: number
  orderBy?: string
  descending?: boolean
}

export interface CollectionStats {
  collection: string
  record_count: string
  segment_count: string
  dirty_entries: string
  size_bytes: string
}

export interface ApiError {
  status: number
  message: string
}
```

- [ ] **Step 2: Create `clients/web/src/api/client.ts`**

```typescript
import type { CollectionStats, DBRecord, Filter, FindRequest, WatchEvent } from './types'

/** Converts our FilterOp to the proto enum string the REST gateway expects. */
const OP_MAP: Record<string, string> = {
  eq: 'EQ', neq: 'NEQ', gt: 'GT', gte: 'GTE',
  lt: 'LT', lte: 'LTE', contains: 'CONTAINS', regex: 'REGEX',
}

function filterToWire(f: Filter): object {
  if ('and' in f) return { and: { filters: f.and.map(filterToWire) } }
  if ('or' in f) return { or: { filters: f.or.map(filterToWire) } }
  return { field: { field: f.field, op: OP_MAP[f.op] ?? 'EQ', value: f.value } }
}

function toRecord(raw: Record<string, unknown>): DBRecord {
  return {
    id: String(raw.id),
    data: (raw.data as Record<string, unknown>) ?? {},
    date_added: raw.date_added ? String(raw.date_added) : undefined,
    date_modified: raw.date_modified ? String(raw.date_modified) : undefined,
  }
}

/** Parse the NDJSON streaming response from grpc-gateway server-streaming RPCs.
 *  Each line is: {"result": <message>} or {"error": ...}
 */
async function parseNdjsonResults<T>(res: Response): Promise<T[]> {
  const text = await res.text()
  const results: T[] = []
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const parsed = JSON.parse(trimmed) as { result?: T; error?: { message: string } }
    if (parsed.error) throw new Error(parsed.error.message)
    if (parsed.result !== undefined) results.push(parsed.result)
  }
  return results
}

export class FileDBClient {
  constructor(
    private readonly baseUrl: string,
    private readonly apiKey: string,
  ) {}

  private headers(): HeadersInit {
    return { 'Content-Type': 'application/json', 'x-api-key': this.apiKey }
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await fetch(`${this.baseUrl}${path}`, {
      method,
      headers: this.headers(),
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
    if (!res.ok) {
      const text = await res.text().catch(() => res.statusText)
      throw Object.assign(new Error(text), { status: res.status })
    }
    return res.json() as Promise<T>
  }

  // --- Collections ---

  async listCollections(): Promise<string[]> {
    const res = await this.request<{ names?: string[] }>('GET', '/v1/collections')
    return res.names ?? []
  }

  async createCollection(name: string): Promise<void> {
    await this.request('POST', '/v1/collections', { name })
  }

  async dropCollection(name: string): Promise<void> {
    await this.request('DELETE', `/v1/collections/${encodeURIComponent(name)}`)
  }

  // --- Records ---

  async insert(collection: string, data: Record<string, unknown>): Promise<string> {
    const res = await this.request<{ id: string }>('POST', `/v1/${encodeURIComponent(collection)}/records`, { data })
    return String(res.id)
  }

  async findById(collection: string, id: string): Promise<DBRecord> {
    const res = await this.request<{ record: Record<string, unknown> }>(
      'GET',
      `/v1/${encodeURIComponent(collection)}/records/${id}`,
    )
    return toRecord(res.record)
  }

  async find(collection: string, req: FindRequest = {}): Promise<DBRecord[]> {
    const body: Record<string, unknown> = { collection }
    if (req.filter) body.filter = filterToWire(req.filter)
    if (req.limit !== undefined) body.limit = req.limit
    if (req.offset !== undefined) body.offset = req.offset
    if (req.orderBy) body.order_by = req.orderBy
    if (req.descending) body.descending = req.descending

    const res = await fetch(`${this.baseUrl}/v1/${encodeURIComponent(collection)}/records/find`, {
      method: 'POST',
      headers: this.headers(),
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const text = await res.text().catch(() => res.statusText)
      throw Object.assign(new Error(text), { status: res.status })
    }
    const rows = await parseNdjsonResults<{ record: Record<string, unknown> }>(res)
    return rows.map((r) => toRecord(r.record))
  }

  async update(collection: string, id: string, data: Record<string, unknown>): Promise<void> {
    await this.request('PUT', `/v1/${encodeURIComponent(collection)}/records/${id}`, { data })
  }

  async deleteRecord(collection: string, id: string): Promise<void> {
    await this.request('DELETE', `/v1/${encodeURIComponent(collection)}/records/${id}`)
  }

  // --- Indexes ---

  async listIndexes(collection: string): Promise<string[]> {
    const res = await this.request<{ fields?: string[] }>('GET', `/v1/${encodeURIComponent(collection)}/indexes`)
    return res.fields ?? []
  }

  async ensureIndex(collection: string, field: string): Promise<void> {
    await this.request('POST', `/v1/${encodeURIComponent(collection)}/indexes`, { field })
  }

  async dropIndex(collection: string, field: string): Promise<void> {
    await this.request('DELETE', `/v1/${encodeURIComponent(collection)}/indexes/${encodeURIComponent(field)}`)
  }

  // --- Stats ---

  async collectionStats(collection: string): Promise<CollectionStats> {
    return this.request<CollectionStats>('GET', `/v1/${encodeURIComponent(collection)}/stats`)
  }

  // --- Watch (streaming) ---

  /** Opens a streaming connection to the Watch endpoint.
   *  Calls onEvent for each event. Returns a cancel function to stop watching.
   */
  watch(
    collection: string,
    onEvent: (e: WatchEvent) => void,
    onError?: (e: Error) => void,
  ): () => void {
    const controller = new AbortController()

    const run = async () => {
      try {
        const res = await fetch(`${this.baseUrl}/v1/${encodeURIComponent(collection)}/watch`, {
          method: 'POST',
          headers: this.headers(),
          body: JSON.stringify({ collection }),
          signal: controller.signal,
        })
        if (!res.ok || !res.body) {
          const text = await res.text().catch(() => res.statusText)
          throw Object.assign(new Error(text), { status: res.status })
        }
        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          const lines = buffer.split('\n')
          buffer = lines.pop() ?? ''
          for (const line of lines) {
            const trimmed = line.trim()
            if (!trimmed) continue
            try {
              const parsed = JSON.parse(trimmed) as { result?: Record<string, unknown> }
              if (parsed.result) {
                const r = parsed.result
                onEvent({
                  op: String(r.op) as WatchEvent['op'],
                  collection: String(r.collection),
                  record: toRecord((r.record as Record<string, unknown>) ?? {}),
                  ts: r.ts ? String(r.ts) : undefined,
                })
              }
            } catch {
              // skip malformed lines
            }
          }
        }
      } catch (err) {
        if ((err as Error).name !== 'AbortError') {
          onError?.(err as Error)
        }
      }
    }

    run()
    return () => controller.abort()
  }
}
```

- [ ] **Step 3: Write the failing tests**

Create `clients/web/src/api/client.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { FileDBClient } from './client'

const mockFetch = vi.fn()
globalThis.fetch = mockFetch

function okJson(body: unknown) {
  return Promise.resolve({
    ok: true,
    status: 200,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(JSON.stringify(body)),
  } as Response)
}

function okNdjson(lines: unknown[]) {
  const text = lines.map((l) => JSON.stringify(l)).join('\n') + '\n'
  return Promise.resolve({
    ok: true,
    status: 200,
    json: () => Promise.resolve({}),
    text: () => Promise.resolve(text),
    body: null,
  } as unknown as Response)
}

function errResponse(status: number, message: string) {
  return Promise.resolve({
    ok: false,
    status,
    statusText: message,
    text: () => Promise.resolve(message),
  } as Response)
}

describe('FileDBClient', () => {
  let client: FileDBClient

  beforeEach(() => {
    client = new FileDBClient('http://localhost:8080', 'test-key')
    mockFetch.mockReset()
  })

  it('listCollections returns names array', async () => {
    mockFetch.mockReturnValue(okJson({ names: ['users', 'products'] }))
    const result = await client.listCollections()
    expect(result).toEqual(['users', 'products'])
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/v1/collections',
      expect.objectContaining({ method: 'GET' }),
    )
  })

  it('listCollections returns empty array when names is absent', async () => {
    mockFetch.mockReturnValue(okJson({}))
    expect(await client.listCollections()).toEqual([])
  })

  it('createCollection POSTs the name', async () => {
    mockFetch.mockReturnValue(okJson({ name: 'users' }))
    await client.createCollection('users')
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/v1/collections',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ name: 'users' }),
      }),
    )
  })

  it('dropCollection DELETEs the collection', async () => {
    mockFetch.mockReturnValue(okJson({ ok: true }))
    await client.dropCollection('users')
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/v1/collections/users',
      expect.objectContaining({ method: 'DELETE' }),
    )
  })

  it('insert sends data and returns id', async () => {
    mockFetch.mockReturnValue(okJson({ id: '42' }))
    const id = await client.insert('users', { name: 'Alice' })
    expect(id).toBe('42')
    expect(mockFetch).toHaveBeenCalledWith(
      'http://localhost:8080/v1/users/records',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ data: { name: 'Alice' } }),
      }),
    )
  })

  it('find parses NDJSON results', async () => {
    mockFetch.mockReturnValue(
      okNdjson([
        { result: { record: { id: '1', data: { name: 'Alice' } } } },
        { result: { record: { id: '2', data: { name: 'Bob' } } } },
      ]),
    )
    const records = await client.find('users', { limit: 20 })
    expect(records).toHaveLength(2)
    expect(records[0].id).toBe('1')
    expect(records[0].data).toEqual({ name: 'Alice' })
    expect(records[1].id).toBe('2')
  })

  it('find sends filter in wire format', async () => {
    mockFetch.mockReturnValue(okNdjson([]))
    await client.find('users', { filter: { field: 'name', op: 'eq', value: 'Alice' } })
    const body = JSON.parse(mockFetch.mock.calls[0][1].body as string)
    expect(body.filter).toEqual({ field: { field: 'name', op: 'EQ', value: 'Alice' } })
  })

  it('find sends AND filter', async () => {
    mockFetch.mockReturnValue(okNdjson([]))
    await client.find('users', {
      filter: {
        and: [
          { field: 'age', op: 'gt', value: '18' },
          { field: 'role', op: 'eq', value: 'admin' },
        ],
      },
    })
    const body = JSON.parse(mockFetch.mock.calls[0][1].body as string)
    expect(body.filter.and.filters).toHaveLength(2)
  })

  it('throws on non-ok response', async () => {
    mockFetch.mockReturnValue(errResponse(401, 'Unauthorized'))
    await expect(client.listCollections()).rejects.toThrow('Unauthorized')
  })

  it('collectionStats returns typed stats', async () => {
    const stats = { collection: 'users', record_count: '142', segment_count: '3', dirty_entries: '18', size_bytes: '2400000' }
    mockFetch.mockReturnValue(okJson(stats))
    const result = await client.collectionStats('users')
    expect(result.record_count).toBe('142')
    expect(result.size_bytes).toBe('2400000')
  })

  it('sends x-api-key header on all requests', async () => {
    mockFetch.mockReturnValue(okJson({ names: [] }))
    await client.listCollections()
    expect(mockFetch.mock.calls[0][1].headers['x-api-key']).toBe('test-key')
  })

  it('encodes collection names in URLs', async () => {
    mockFetch.mockReturnValue(okNdjson([]))
    await client.find('my collection', {})
    expect(mockFetch.mock.calls[0][0]).toContain('my%20collection')
  })
})
```

- [ ] **Step 4: Run tests to verify they fail**

```bash
cd clients/web
npm run test
```

Expected: FAIL — `FileDBClient` not implemented yet (or many test errors since client.ts doesn't exist yet). If client.ts already exists from step 2, tests may pass — that is fine, proceed.

- [ ] **Step 5: Run tests to verify they pass**

```bash
npm run test
```

Expected: all 11 tests pass.

- [ ] **Step 6: Commit**

```bash
cd ../..
git add clients/web/src/api/
git commit -m "feat(web): add REST API client with tests"
```

---

## Task 4: Toast context and component

**Files:**
- Create: `clients/web/src/contexts/ToastContext.tsx`
- Create: `clients/web/src/components/Toast.tsx`

- [ ] **Step 1: Create `clients/web/src/contexts/ToastContext.tsx`**

```tsx
import { createContext, useCallback, useContext, useRef, useState } from 'react'

export type ToastType = 'success' | 'error' | 'info'

export interface ToastMessage {
  id: number
  type: ToastType
  title: string
  body?: string
}

interface ToastCtx {
  toasts: ToastMessage[]
  addToast: (type: ToastType, title: string, body?: string) => void
  removeToast: (id: number) => void
}

const ToastContext = createContext<ToastCtx | null>(null)

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<ToastMessage[]>([])
  const nextId = useRef(0)

  const removeToast = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const addToast = useCallback(
    (type: ToastType, title: string, body?: string) => {
      const id = ++nextId.current
      setToasts((prev) => {
        const next = [...prev, { id, type, title, body }]
        return next.slice(-3) // keep max 3
      })
      setTimeout(() => removeToast(id), 4000)
    },
    [removeToast],
  )

  return (
    <ToastContext.Provider value={{ toasts, addToast, removeToast }}>
      {children}
    </ToastContext.Provider>
  )
}

export function useToast() {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used inside ToastProvider')
  return ctx
}
```

- [ ] **Step 2: Create `clients/web/src/components/Toast.tsx`**

```tsx
import { useToast } from '../contexts/ToastContext'

const BG: Record<string, string> = {
  success: 'bg-green-950 border-green-800',
  error: 'bg-red-950 border-red-800',
  info: 'bg-blue-950 border-blue-800',
}

const TITLE_COLOR: Record<string, string> = {
  success: 'text-green-400',
  error: 'text-red-400',
  info: 'text-blue-400',
}

const ICON: Record<string, string> = {
  success: '✓',
  error: '✕',
  info: 'ℹ',
}

export function ToastStack() {
  const { toasts, removeToast } = useToast()
  return (
    <div className="fixed bottom-4 right-4 flex flex-col gap-2 z-50 min-w-64 max-w-80">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`flex items-start gap-3 rounded-lg border px-4 py-3 shadow-lg ${BG[t.type]}`}
        >
          <span className={`text-base mt-0.5 ${TITLE_COLOR[t.type]}`}>{ICON[t.type]}</span>
          <div className="flex-1 min-w-0">
            <div className={`font-semibold text-sm ${TITLE_COLOR[t.type]}`}>{t.title}</div>
            {t.body && <div className="text-xs text-gray-400 mt-0.5 break-words">{t.body}</div>}
          </div>
          <button
            className="text-gray-500 hover:text-gray-300 text-sm ml-1 flex-shrink-0"
            onClick={() => removeToast(t.id)}
          >
            ✕
          </button>
        </div>
      ))}
    </div>
  )
}
```

- [ ] **Step 3: Commit**

```bash
git add clients/web/src/contexts/ clients/web/src/components/Toast.tsx
git commit -m "feat(web): add toast context and component"
```

---

## Task 5: App context (connection state)

**Files:**
- Create: `clients/web/src/contexts/AppContext.tsx`

- [ ] **Step 1: Create `clients/web/src/contexts/AppContext.tsx`**

```tsx
import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { FileDBClient } from '../api/client'

const STORAGE_KEY = 'filedb-connection'

interface StoredSettings {
  url: string
  apiKey: string
}

interface AppCtx {
  client: FileDBClient | null
  settings: StoredSettings | null
  connected: boolean
  connect: (url: string, apiKey: string) => Promise<void>
  disconnect: () => void
}

const AppContext = createContext<AppCtx | null>(null)

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [client, setClient] = useState<FileDBClient | null>(null)
  const [settings, setSettings] = useState<StoredSettings | null>(null)
  const [connected, setConnected] = useState(false)

  const connect = useCallback(async (url: string, apiKey: string) => {
    const c = new FileDBClient(url, apiKey)
    // Verify connection by listing collections
    await c.listCollections()
    const stored: StoredSettings = { url, apiKey }
    localStorage.setItem(STORAGE_KEY, JSON.stringify(stored))
    setClient(c)
    setSettings(stored)
    setConnected(true)
  }, [])

  const disconnect = useCallback(() => {
    setClient(null)
    setSettings(null)
    setConnected(false)
  }, [])

  // Auto-connect from localStorage on mount
  useEffect(() => {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return
    try {
      const { url, apiKey } = JSON.parse(raw) as StoredSettings
      connect(url, apiKey).catch(() => {
        // Saved settings no longer valid — stay on connect screen
      })
    } catch {
      // ignore corrupt storage
    }
  }, [connect])

  return (
    <AppContext.Provider value={{ client, settings, connected, connect, disconnect }}>
      {children}
    </AppContext.Provider>
  )
}

export function useApp() {
  const ctx = useContext(AppContext)
  if (!ctx) throw new Error('useApp must be used inside AppProvider')
  return ctx
}
```

- [ ] **Step 2: Commit**

```bash
git add clients/web/src/contexts/AppContext.tsx
git commit -m "feat(web): add app context for connection state"
```

---

## Task 6: App shell, ConnectScreen, and SettingsPanel

**Files:**
- Create: `clients/web/src/components/ConnectScreen.tsx`
- Create: `clients/web/src/components/SettingsPanel.tsx`
- Modify: `clients/web/src/App.tsx`

- [ ] **Step 1: Create `clients/web/src/components/ConnectScreen.tsx`**

```tsx
import { useState } from 'react'
import { useApp } from '../contexts/AppContext'
import { useToast } from '../contexts/ToastContext'

export function ConnectScreen() {
  const { connect } = useApp()
  const { addToast } = useToast()
  const [url, setUrl] = useState('http://localhost:8080')
  const [apiKey, setApiKey] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    try {
      await connect(url.trim(), apiKey.trim())
    } catch {
      addToast('error', 'Connection failed', 'Check the server URL and API key')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gray-950 flex items-center justify-center">
      <form
        onSubmit={handleSubmit}
        className="bg-gray-900 border border-gray-700 rounded-xl p-8 w-80 shadow-2xl"
      >
        <div className="flex items-center gap-2 mb-6">
          <span className="text-brand text-2xl">⬡</span>
          <span className="font-bold text-lg text-gray-100">Connect to FileDB</span>
        </div>
        <label className="block mb-4">
          <span className="text-xs text-gray-400 uppercase tracking-wide">Server URL</span>
          <input
            className="mt-1 w-full"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="http://localhost:8080"
            required
          />
        </label>
        <label className="block mb-6">
          <span className="text-xs text-gray-400 uppercase tracking-wide">API Key</span>
          <input
            className="mt-1 w-full"
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder="dev-key"
          />
        </label>
        <button
          type="submit"
          disabled={loading}
          className="w-full bg-brand hover:bg-blue-600 text-white font-semibold py-2 rounded text-sm disabled:opacity-50 transition-colors"
        >
          {loading ? 'Connecting…' : 'Connect'}
        </button>
        <p className="text-xs text-gray-600 text-center mt-4">Settings saved to localStorage</p>
      </form>
    </div>
  )
}
```

- [ ] **Step 2: Create `clients/web/src/components/SettingsPanel.tsx`**

```tsx
import { useState } from 'react'
import { useApp } from '../contexts/AppContext'
import { useToast } from '../contexts/ToastContext'

interface Props {
  onClose: () => void
}

export function SettingsPanel({ onClose }: Props) {
  const { settings, connect, disconnect } = useApp()
  const { addToast } = useToast()
  const [url, setUrl] = useState(settings?.url ?? 'http://localhost:8080')
  const [apiKey, setApiKey] = useState(settings?.apiKey ?? '')
  const [loading, setLoading] = useState(false)

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    try {
      await connect(url.trim(), apiKey.trim())
      addToast('success', 'Reconnected', url)
      onClose()
    } catch {
      addToast('error', 'Reconnect failed', 'Check the server URL and API key')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end" onClick={onClose}>
      <div
        className="bg-gray-900 border-l border-gray-700 w-80 h-full p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex justify-between items-center mb-6">
          <h2 className="font-semibold text-gray-100">Connection Settings</h2>
          <button className="text-gray-500 hover:text-gray-300" onClick={onClose}>✕</button>
        </div>
        <form onSubmit={handleSave}>
          <label className="block mb-4">
            <span className="text-xs text-gray-400 uppercase tracking-wide">Server URL</span>
            <input className="mt-1 w-full" value={url} onChange={(e) => setUrl(e.target.value)} required />
          </label>
          <label className="block mb-6">
            <span className="text-xs text-gray-400 uppercase tracking-wide">API Key</span>
            <input className="mt-1 w-full" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
          </label>
          <button
            type="submit"
            disabled={loading}
            className="w-full bg-brand hover:bg-blue-600 text-white py-2 rounded text-sm font-semibold disabled:opacity-50 transition-colors"
          >
            {loading ? 'Reconnecting…' : 'Save & Reconnect'}
          </button>
          <button
            type="button"
            className="w-full mt-2 text-red-400 hover:text-red-300 text-sm py-2"
            onClick={disconnect}
          >
            Disconnect
          </button>
        </form>
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Rewrite `clients/web/src/App.tsx`**

```tsx
import { useState } from 'react'
import { AppProvider, useApp } from './contexts/AppContext'
import { ToastProvider } from './contexts/ToastContext'
import { ToastStack } from './components/Toast'
import { ConnectScreen } from './components/ConnectScreen'
import { SettingsPanel } from './components/SettingsPanel'
import { Sidebar } from './components/Sidebar'
import { CollectionView } from './components/CollectionView'

function Shell() {
  const { connected, settings } = useApp()
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [activeCollection, setActiveCollection] = useState<string | null>(null)

  if (!connected) return <ConnectScreen />

  return (
    <div className="flex flex-col h-screen overflow-hidden">
      {/* Top bar */}
      <header className="flex items-center justify-between px-4 py-2 bg-gray-900 border-b border-gray-700 flex-shrink-0">
        <div className="flex items-center gap-3">
          <span className="text-brand text-xl font-bold">⬡ FileDB</span>
          <span className="text-gray-600">|</span>
          <span className="text-gray-400 text-sm">{settings?.url}</span>
          <span className="px-2 py-0.5 rounded-full text-xs bg-green-950 text-green-400 border border-green-800">
            ● connected
          </span>
        </div>
        <button
          className="text-gray-400 hover:text-gray-200 text-sm"
          onClick={() => setSettingsOpen(true)}
        >
          ⚙ Settings
        </button>
      </header>

      {/* Body */}
      <div className="flex flex-1 overflow-hidden">
        <Sidebar activeCollection={activeCollection} onSelectCollection={setActiveCollection} />
        <main className="flex-1 overflow-hidden">
          {activeCollection ? (
            <CollectionView collection={activeCollection} />
          ) : (
            <div className="flex items-center justify-center h-full text-gray-600">
              Select a collection from the sidebar
            </div>
          )}
        </main>
      </div>

      {settingsOpen && <SettingsPanel onClose={() => setSettingsOpen(false)} />}
      <ToastStack />
    </div>
  )
}

export default function App() {
  return (
    <ToastProvider>
      <AppProvider>
        <Shell />
      </AppProvider>
    </ToastProvider>
  )
}
```

- [ ] **Step 4: Create placeholder components so App.tsx compiles**

Create `clients/web/src/components/Sidebar.tsx`:

```tsx
interface Props {
  activeCollection: string | null
  onSelectCollection: (name: string) => void
}

export function Sidebar(_props: Props) {
  return <div className="w-48 bg-gray-900 border-r border-gray-700">Sidebar</div>
}
```

Create `clients/web/src/components/CollectionView.tsx`:

```tsx
interface Props { collection: string }

export function CollectionView({ collection }: Props) {
  return <div className="p-4">CollectionView: {collection}</div>
}
```

- [ ] **Step 5: Verify app compiles and renders**

```bash
cd clients/web && npm run dev
```

Open `http://localhost:5173` — expect the ConnectScreen. Fill in `http://localhost:8080` and your API key. After connecting, expect the app shell with top bar and placeholder sidebar.

- [ ] **Step 6: Commit**

```bash
cd ../..
git add clients/web/src/
git commit -m "feat(web): add app shell, connect screen, settings panel"
```

---

## Task 7: Sidebar and useCollections hook

**Files:**
- Create: `clients/web/src/hooks/useCollections.ts`
- Modify: `clients/web/src/components/Sidebar.tsx`

- [ ] **Step 1: Create `clients/web/src/hooks/useCollections.ts`**

```typescript
import { useCallback, useEffect, useState } from 'react'
import { useApp } from '../contexts/AppContext'
import { useToast } from '../contexts/ToastContext'

export interface CollectionEntry {
  name: string
}

export function useCollections() {
  const { client } = useApp()
  const { addToast } = useToast()
  const [collections, setCollections] = useState<string[]>([])
  const [loading, setLoading] = useState(false)

  const refresh = useCallback(async () => {
    if (!client) return
    setLoading(true)
    try {
      const names = await client.listCollections()
      setCollections(names)
    } catch (err) {
      addToast('error', 'Failed to load collections', (err as Error).message)
    } finally {
      setLoading(false)
    }
  }, [client, addToast])

  useEffect(() => { refresh() }, [refresh])

  const createCollection = useCallback(
    async (name: string) => {
      if (!client) return
      await client.createCollection(name)
      addToast('success', 'Collection created', name)
      await refresh()
    },
    [client, addToast, refresh],
  )

  const dropCollection = useCallback(
    async (name: string) => {
      if (!client) return
      await client.dropCollection(name)
      addToast('success', 'Collection dropped', name)
      await refresh()
    },
    [client, addToast, refresh],
  )

  return { collections, loading, refresh, createCollection, dropCollection }
}
```

- [ ] **Step 2: Rewrite `clients/web/src/components/Sidebar.tsx`**

```tsx
import { useState } from 'react'
import { useCollections } from '../hooks/useCollections'

interface Props {
  activeCollection: string | null
  onSelectCollection: (name: string) => void
}

export function Sidebar({ activeCollection, onSelectCollection }: Props) {
  const { collections, loading, createCollection, dropCollection } = useCollections()
  const [showCreate, setShowCreate] = useState(false)
  const [newName, setNewName] = useState('')
  const [showDropConfirm, setShowDropConfirm] = useState<string | null>(null)
  const [confirmInput, setConfirmInput] = useState('')

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    const name = newName.trim()
    if (!name) return
    try {
      await createCollection(name)
      setNewName('')
      setShowCreate(false)
      onSelectCollection(name)
    } catch (err) {
      alert((err as Error).message)
    }
  }

  const handleDrop = async () => {
    if (!showDropConfirm || confirmInput !== showDropConfirm) return
    try {
      await dropCollection(showDropConfirm)
      setShowDropConfirm(null)
      setConfirmInput('')
      if (activeCollection === showDropConfirm) onSelectCollection(collections.find((c) => c !== showDropConfirm) ?? '')
    } catch (err) {
      alert((err as Error).message)
    }
  }

  return (
    <aside className="w-48 bg-gray-900 border-r border-gray-700 flex flex-col flex-shrink-0 overflow-y-auto">
      <div className="px-3 pt-3 pb-1">
        <span className="text-xs text-gray-500 uppercase tracking-wider">Collections</span>
      </div>

      {loading && <div className="px-3 py-2 text-xs text-gray-600">Loading…</div>}

      {collections.map((name) => (
        <button
          key={name}
          className={`flex items-center justify-between px-3 py-2 text-sm text-left w-full group transition-colors ${
            activeCollection === name
              ? 'bg-gray-800 border-l-2 border-brand text-brand'
              : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200'
          }`}
          onClick={() => onSelectCollection(name)}
        >
          <span className="truncate">{activeCollection === name ? '● ' : '  '}{name}</span>
          <button
            className="text-gray-600 hover:text-red-400 opacity-0 group-hover:opacity-100 text-xs ml-1 flex-shrink-0"
            title={`Drop ${name}`}
            onClick={(e) => { e.stopPropagation(); setShowDropConfirm(name); setConfirmInput('') }}
          >
            ✕
          </button>
        </button>
      ))}

      <div className="mt-auto border-t border-gray-800 p-2">
        {showCreate ? (
          <form onSubmit={handleCreate} className="flex gap-1">
            <input
              autoFocus
              className="flex-1 text-xs py-1 px-2"
              placeholder="name"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              pattern="[a-zA-Z0-9_]+"
              title="Letters, numbers, underscores only"
              required
            />
            <button type="submit" className="text-green-400 text-xs px-1">✓</button>
            <button type="button" className="text-gray-500 text-xs px-1" onClick={() => setShowCreate(false)}>✕</button>
          </form>
        ) : (
          <button
            className="w-full text-left text-xs text-gray-600 hover:text-gray-400 py-1 px-1 transition-colors"
            onClick={() => setShowCreate(true)}
          >
            + New Collection
          </button>
        )}
      </div>

      {/* Drop confirmation modal */}
      {showDropConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
          <div className="bg-gray-900 border border-red-900 rounded-xl p-6 w-72 shadow-2xl">
            <h3 className="text-red-400 font-semibold mb-2">⚠ Drop Collection</h3>
            <p className="text-gray-400 text-sm mb-4">
              This will permanently delete <span className="text-gray-200 font-mono">{showDropConfirm}</span> and all its records.
            </p>
            <p className="text-gray-500 text-xs mb-2">
              Type <span className="text-gray-300 font-mono">{showDropConfirm}</span> to confirm:
            </p>
            <input
              autoFocus
              className="w-full mb-4 border-red-900"
              value={confirmInput}
              onChange={(e) => setConfirmInput(e.target.value)}
            />
            <div className="flex gap-2">
              <button
                className="flex-1 bg-red-950 border border-red-800 text-red-400 hover:bg-red-900 py-1.5 rounded text-sm transition-colors disabled:opacity-40"
                disabled={confirmInput !== showDropConfirm}
                onClick={handleDrop}
              >
                Drop
              </button>
              <button
                className="flex-1 bg-gray-800 text-gray-400 hover:bg-gray-700 py-1.5 rounded text-sm transition-colors"
                onClick={() => { setShowDropConfirm(null); setConfirmInput('') }}
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
    </aside>
  )
}
```

- [ ] **Step 3: Verify sidebar works**

Start the FileDB server (`make run` in the project root) and the dev server (`npm run dev` in `clients/web`). Open `http://localhost:5173`, connect, and confirm collections appear in the sidebar. Click a collection to select it. Try "+ New Collection".

- [ ] **Step 4: Commit**

```bash
git add clients/web/src/hooks/useCollections.ts clients/web/src/components/Sidebar.tsx
git commit -m "feat(web): sidebar with collection list, create, drop"
```

---

## Task 8: FilterBar component

**Files:**
- Create: `clients/web/src/components/FilterBar.tsx`

- [ ] **Step 1: Create `clients/web/src/components/FilterBar.tsx`**

```tsx
import { useState } from 'react'
import type { Filter, FilterOp } from '../api/types'

const OPS: FilterOp[] = ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'contains', 'regex']

interface FilterRow {
  type: 'AND' | 'OR'
  field: string
  op: FilterOp
  value: string
}

export interface FilterBarState {
  rows: FilterRow[]
  orderBy: string
  descending: boolean
  limit: number
}

interface Props {
  onRun: (state: FilterBarState) => void
}

function buildFilter(rows: FilterRow[]): Filter | undefined {
  if (rows.length === 0) return undefined
  const fields = rows.map((r): Filter => ({ field: r.field, op: r.op, value: r.value }))
  if (fields.length === 1) return fields[0]
  // Group by AND/OR: first row has no type separator, subsequent rows have AND/OR
  // Simple approach: all AND rows → and{}, or OR rows → or{}. Mixed: wrap in and/or by sequence.
  const andRows = rows.filter((_, i) => i === 0 || rows[i].type === 'AND')
  const orRows = rows.filter((_, i) => i > 0 && rows[i].type === 'OR')
  if (orRows.length === 0) {
    return { and: fields }
  }
  return { or: fields }
}

export function FilterBar({ onRun }: Props) {
  const [rows, setRows] = useState<FilterRow[]>([])
  const [orderBy, setOrderBy] = useState('')
  const [descending, setDescending] = useState(false)
  const [limit, setLimit] = useState(20)

  const addRow = (type: 'AND' | 'OR') => {
    setRows((prev) => [...prev, { type, field: '', op: 'eq', value: '' }])
  }

  const updateRow = (i: number, patch: Partial<FilterRow>) => {
    setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, ...patch } : r)))
  }

  const removeRow = (i: number) => {
    setRows((prev) => prev.filter((_, idx) => idx !== i))
  }

  const handleRun = () => {
    onRun({ rows, orderBy, descending, limit })
  }

  const handleClear = () => {
    setRows([])
    setOrderBy('')
    setDescending(false)
    setLimit(20)
    onRun({ rows: [], orderBy: '', descending: false, limit: 20 })
  }

  return (
    <div className="bg-gray-900 border-b border-gray-700 px-4 py-3 space-y-2">
      {rows.map((row, i) => (
        <div key={i} className="flex items-center gap-2">
          {i > 0 && (
            <span
              className={`text-xs px-2 py-0.5 rounded font-mono ${
                row.type === 'AND' ? 'bg-blue-950 text-blue-300' : 'bg-purple-950 text-purple-300'
              }`}
            >
              {row.type}
            </span>
          )}
          <input
            className="w-28 text-xs"
            placeholder="field"
            value={row.field}
            onChange={(e) => updateRow(i, { field: e.target.value })}
          />
          <select
            className="w-24 text-xs"
            value={row.op}
            onChange={(e) => updateRow(i, { op: e.target.value as FilterOp })}
          >
            {OPS.map((op) => <option key={op} value={op}>{op}</option>)}
          </select>
          <input
            className="w-36 text-xs"
            placeholder="value"
            value={row.value}
            onChange={(e) => updateRow(i, { value: e.target.value })}
          />
          <button className="text-gray-600 hover:text-red-400 text-xs" onClick={() => removeRow(i)}>✕</button>
        </div>
      ))}

      <div className="flex items-center gap-2 flex-wrap">
        <button
          className="text-xs px-2 py-1 bg-blue-950 border border-blue-800 text-blue-300 hover:bg-blue-900 rounded transition-colors"
          onClick={() => addRow('AND')}
        >
          + AND
        </button>
        <button
          className="text-xs px-2 py-1 bg-purple-950 border border-purple-800 text-purple-300 hover:bg-purple-900 rounded transition-colors"
          onClick={() => addRow('OR')}
        >
          + OR
        </button>

        <span className="text-gray-600 text-xs ml-2">Order by:</span>
        <input
          className="w-24 text-xs"
          placeholder="field"
          value={orderBy}
          onChange={(e) => setOrderBy(e.target.value)}
        />
        <select
          className="w-20 text-xs"
          value={descending ? 'desc' : 'asc'}
          onChange={(e) => setDescending(e.target.value === 'desc')}
        >
          <option value="asc">asc</option>
          <option value="desc">desc</option>
        </select>

        <span className="text-gray-600 text-xs ml-2">Limit:</span>
        <input
          className="w-16 text-xs"
          type="number"
          min={1}
          max={1000}
          value={limit}
          onChange={(e) => setLimit(Number(e.target.value))}
        />

        <button
          className="ml-auto text-xs px-3 py-1 bg-brand hover:bg-blue-600 text-white rounded transition-colors"
          onClick={handleRun}
        >
          Run
        </button>
        <button
          className="text-xs px-3 py-1 bg-gray-800 hover:bg-gray-700 text-gray-400 rounded transition-colors"
          onClick={handleClear}
        >
          Clear
        </button>
      </div>
    </div>
  )
}

export function filterBarStateToRequest(state: FilterBarState) {
  return {
    filter: buildFilter(state.rows),
    orderBy: state.orderBy || undefined,
    descending: state.descending || undefined,
    limit: state.limit,
  }
}
```

- [ ] **Step 2: Commit**

```bash
git add clients/web/src/components/FilterBar.tsx
git commit -m "feat(web): add FilterBar component"
```

---

## Task 9: BrowseTab and useRecords hook

**Files:**
- Create: `clients/web/src/hooks/useRecords.ts`
- Create: `clients/web/src/components/BrowseTab.tsx`

- [ ] **Step 1: Create `clients/web/src/hooks/useRecords.ts`**

```typescript
import { useCallback, useState } from 'react'
import type { DBRecord, FindRequest } from '../api/types'
import { useApp } from '../contexts/AppContext'
import { useToast } from '../contexts/ToastContext'

export function useRecords(collection: string) {
  const { client } = useApp()
  const { addToast } = useToast()
  const [records, setRecords] = useState<DBRecord[]>([])
  const [loading, setLoading] = useState(false)
  const [offset, setOffset] = useState(0)
  const [hasMore, setHasMore] = useState(false)
  const [lastReq, setLastReq] = useState<FindRequest>({ limit: 20, offset: 0 })

  const fetch = useCallback(
    async (req: FindRequest) => {
      if (!client) return
      setLoading(true)
      try {
        const results = await client.find(collection, req)
        setRecords(results)
        setOffset(req.offset ?? 0)
        setLastReq(req)
        setHasMore(results.length === (req.limit ?? 20))
      } catch (err) {
        addToast('error', 'Query failed', (err as Error).message)
      } finally {
        setLoading(false)
      }
    },
    [client, collection, addToast],
  )

  const nextPage = useCallback(() => {
    const limit = lastReq.limit ?? 20
    fetch({ ...lastReq, offset: offset + limit })
  }, [fetch, lastReq, offset])

  const prevPage = useCallback(() => {
    const limit = lastReq.limit ?? 20
    fetch({ ...lastReq, offset: Math.max(0, offset - limit) })
  }, [fetch, lastReq, offset])

  const deleteRecord = useCallback(
    async (id: string) => {
      if (!client) return
      await client.deleteRecord(collection, id)
      addToast('success', 'Record deleted', `id: ${id}`)
      await fetch(lastReq)
    },
    [client, collection, addToast, fetch, lastReq],
  )

  return { records, loading, offset, hasMore, fetch, nextPage, prevPage, deleteRecord }
}
```

- [ ] **Step 2: Create `clients/web/src/components/BrowseTab.tsx`**

```tsx
import { useEffect, useState } from 'react'
import { FilterBar, filterBarStateToRequest } from './FilterBar'
import type { FilterBarState } from './FilterBar'
import { RecordModal } from './RecordModal'
import { useRecords } from '../hooks/useRecords'
import type { DBRecord } from '../api/types'

interface Props { collection: string }

/** Returns all unique keys from a list of records, excluding metadata fields. */
function deriveColumns(records: DBRecord[]): string[] {
  const keys = new Set<string>()
  for (const r of records) {
    for (const k of Object.keys(r.data)) keys.add(k)
  }
  return Array.from(keys).slice(0, 6) // cap at 6 data columns to avoid overflow
}

function formatValue(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'object') return '{…}'
  return String(v)
}

export function BrowseTab({ collection }: Props) {
  const { records, loading, offset, hasMore, fetch, nextPage, prevPage, deleteRecord } = useRecords(collection)
  const [modalRecord, setModalRecord] = useState<DBRecord | null | 'new'>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)

  // Initial load
  useEffect(() => { fetch({ limit: 20, offset: 0 }) }, [collection])  // eslint-disable-line react-hooks/exhaustive-deps

  const handleRun = (state: FilterBarState) => {
    fetch({ ...filterBarStateToRequest(state), offset: 0 })
  }

  const handleDelete = async (id: string) => {
    if (!confirm(`Delete record id:${id}?`)) return
    await deleteRecord(id)
  }

  const columns = deriveColumns(records)
  const page = Math.floor(offset / 20) + 1

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between px-4 py-2 bg-gray-900 border-b border-gray-700">
        <span className="text-xs text-gray-500">Browse</span>
        <button
          className="text-xs px-3 py-1 bg-green-950 border border-green-800 text-green-400 hover:bg-green-900 rounded transition-colors"
          onClick={() => setModalRecord('new')}
        >
          + Insert
        </button>
      </div>

      <FilterBar onRun={handleRun} />

      <div className="flex-1 overflow-auto">
        {loading ? (
          <div className="p-4 text-xs text-gray-600">Loading…</div>
        ) : (
          <table className="w-full text-xs text-left font-mono">
            <thead className="bg-gray-900 text-gray-500 uppercase text-[10px] tracking-wider sticky top-0">
              <tr>
                <th className="px-3 py-2">ID</th>
                {columns.map((col) => <th key={col} className="px-3 py-2">{col}</th>)}
                <th className="px-3 py-2">created</th>
                <th className="px-3 py-2">actions</th>
              </tr>
            </thead>
            <tbody>
              {records.map((r) => (
                <>
                  <tr
                    key={r.id}
                    className="border-b border-gray-800 hover:bg-gray-800/50 cursor-pointer"
                    onClick={() => setExpandedId(expandedId === r.id ? null : r.id)}
                  >
                    <td className="px-3 py-2 text-brand">{r.id}</td>
                    {columns.map((col) => (
                      <td key={col} className="px-3 py-2 text-gray-300 max-w-32 truncate">
                        {formatValue(r.data[col])}
                      </td>
                    ))}
                    <td className="px-3 py-2 text-gray-600">
                      {r.date_added ? new Date(r.date_added).toLocaleDateString() : '—'}
                    </td>
                    <td className="px-3 py-2">
                      <button
                        className="text-blue-400 hover:text-blue-300 mr-3"
                        title="Edit"
                        onClick={(e) => { e.stopPropagation(); setModalRecord(r) }}
                      >
                        ✏
                      </button>
                      <button
                        className="text-red-500 hover:text-red-400"
                        title="Delete"
                        onClick={(e) => { e.stopPropagation(); handleDelete(r.id) }}
                      >
                        ✕
                      </button>
                    </td>
                  </tr>
                  {expandedId === r.id && (
                    <tr key={`${r.id}-expand`} className="bg-gray-800/30">
                      <td colSpan={columns.length + 3} className="px-4 py-2">
                        <pre className="text-xs text-gray-300 whitespace-pre-wrap break-all">
                          {JSON.stringify(r.data, null, 2)}
                        </pre>
                      </td>
                    </tr>
                  )}
                </>
              ))}
              {records.length === 0 && (
                <tr>
                  <td colSpan={columns.length + 3} className="px-3 py-8 text-center text-gray-600">
                    No records
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>

      {/* Pagination */}
      <div className="flex items-center gap-3 px-4 py-2 bg-gray-900 border-t border-gray-700 text-xs text-gray-500 flex-shrink-0">
        <button
          disabled={offset === 0}
          className="hover:text-gray-300 disabled:opacity-30 transition-colors"
          onClick={prevPage}
        >
          ← prev
        </button>
        <span className="bg-gray-800 px-2 py-0.5 rounded text-brand">{page}</span>
        <button
          disabled={!hasMore}
          className="hover:text-gray-300 disabled:opacity-30 transition-colors"
          onClick={nextPage}
        >
          next →
        </button>
        <span className="ml-auto">{records.length} records on this page</span>
      </div>

      {modalRecord && (
        <RecordModal
          collection={collection}
          record={modalRecord === 'new' ? null : modalRecord}
          onClose={() => setModalRecord(null)}
          onSaved={() => { setModalRecord(null); fetch({ limit: 20, offset }) }}
        />
      )}
    </div>
  )
}
```

- [ ] **Step 3: Create placeholder RecordModal so BrowseTab compiles**

```tsx
// clients/web/src/components/RecordModal.tsx (placeholder)
import type { DBRecord } from '../api/types'
interface Props { collection: string; record: DBRecord | null; onClose: () => void; onSaved: () => void }
export function RecordModal({ onClose }: Props) {
  return <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
    <div className="bg-gray-900 p-6 rounded-xl border border-gray-700 w-80">
      RecordModal placeholder <button onClick={onClose}>Close</button>
    </div>
  </div>
}
```

- [ ] **Step 4: Verify browse tab renders**

With server running, connect in the UI, select a collection — you should see the browse tab with a filter bar and record table.

- [ ] **Step 5: Commit**

```bash
git add clients/web/src/hooks/useRecords.ts clients/web/src/components/BrowseTab.tsx clients/web/src/components/RecordModal.tsx
git commit -m "feat(web): add BrowseTab with filter bar, record table, pagination"
```

---

## Task 10: RecordModal (insert/edit)

**Files:**
- Modify: `clients/web/src/components/RecordModal.tsx`

- [ ] **Step 1: Rewrite `clients/web/src/components/RecordModal.tsx`**

```tsx
import { useState } from 'react'
import type { DBRecord } from '../api/types'
import { useApp } from '../contexts/AppContext'
import { useToast } from '../contexts/ToastContext'

interface Props {
  collection: string
  record: DBRecord | null   // null = insert mode
  onClose: () => void
  onSaved: () => void
}

interface KVRow { key: string; value: string }

function recordToKV(record: DBRecord): KVRow[] {
  return Object.entries(record.data).map(([key, value]) => ({
    key,
    value: typeof value === 'string' ? value : JSON.stringify(value),
  }))
}

function kvToData(rows: KVRow[]): Record<string, unknown> {
  const data: Record<string, unknown> = {}
  for (const { key, value } of rows) {
    if (!key.trim()) continue
    try { data[key] = JSON.parse(value) } catch { data[key] = value }
  }
  return data
}

export function RecordModal({ collection, record, onClose, onSaved }: Props) {
  const { client } = useApp()
  const { addToast } = useToast()
  const isEdit = record !== null

  const [mode, setMode] = useState<'json' | 'form'>('form')
  const [jsonText, setJsonText] = useState(() =>
    isEdit ? JSON.stringify(record!.data, null, 2) : '{\n  \n}',
  )
  const [kvRows, setKvRows] = useState<KVRow[]>(() =>
    isEdit ? recordToKV(record!) : [{ key: '', value: '' }],
  )
  const [jsonError, setJsonError] = useState('')
  const [loading, setLoading] = useState(false)

  const addKvRow = () => setKvRows((prev) => [...prev, { key: '', value: '' }])
  const removeKvRow = (i: number) => setKvRows((prev) => prev.filter((_, idx) => idx !== i))
  const updateKvRow = (i: number, patch: Partial<KVRow>) =>
    setKvRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, ...patch } : r)))

  const switchToJson = () => {
    setJsonText(JSON.stringify(kvToData(kvRows), null, 2))
    setMode('json')
  }

  const switchToForm = () => {
    try {
      const parsed = JSON.parse(jsonText) as Record<string, unknown>
      setKvRows(Object.entries(parsed).map(([key, value]) => ({
        key,
        value: typeof value === 'string' ? value : JSON.stringify(value),
      })))
      setJsonError('')
      setMode('form')
    } catch {
      setJsonError('Invalid JSON — fix before switching')
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!client) return
    let data: Record<string, unknown>
    if (mode === 'json') {
      try { data = JSON.parse(jsonText) as Record<string, unknown> }
      catch { setJsonError('Invalid JSON'); return }
    } else {
      data = kvToData(kvRows)
    }
    setLoading(true)
    try {
      if (isEdit) {
        await client.update(collection, record!.id, data)
        addToast('success', 'Record updated', `id: ${record!.id}`)
      } else {
        const id = await client.insert(collection, data)
        addToast('success', 'Record inserted', `id: ${id}`)
      }
      onSaved()
    } catch (err) {
      addToast('error', isEdit ? 'Update failed' : 'Insert failed', (err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4">
      <div className="bg-gray-900 border border-gray-700 rounded-xl w-full max-w-md shadow-2xl max-h-[80vh] flex flex-col">
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700 flex-shrink-0">
          <h2 className="font-semibold text-gray-100">{isEdit ? `Edit Record ${record!.id}` : 'Insert Record'}</h2>
          <div className="flex items-center gap-3">
            <button
              className="text-xs text-brand hover:text-blue-400"
              onClick={mode === 'json' ? switchToForm : switchToJson}
            >
              ⇄ {mode === 'json' ? 'Form view' : 'JSON view'}
            </button>
            <button className="text-gray-500 hover:text-gray-300" onClick={onClose}>✕</button>
          </div>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col flex-1 overflow-hidden">
          <div className="flex-1 overflow-y-auto px-5 py-4">
            {mode === 'json' ? (
              <div>
                <textarea
                  className="w-full h-48 font-mono text-xs bg-gray-950 border border-gray-700 rounded p-2 text-gray-200 resize-none focus:border-brand outline-none"
                  value={jsonText}
                  onChange={(e) => { setJsonText(e.target.value); setJsonError('') }}
                  spellCheck={false}
                />
                {jsonError && <p className="text-red-400 text-xs mt-1">{jsonError}</p>}
              </div>
            ) : (
              <div className="space-y-2">
                {kvRows.map((row, i) => (
                  <div key={i} className="flex gap-2 items-center">
                    <input
                      className="w-28 text-xs"
                      placeholder="field"
                      value={row.key}
                      onChange={(e) => updateKvRow(i, { key: e.target.value })}
                    />
                    <input
                      className="flex-1 text-xs"
                      placeholder="value"
                      value={row.value}
                      onChange={(e) => updateKvRow(i, { value: e.target.value })}
                    />
                    <button
                      type="button"
                      className="text-gray-600 hover:text-red-400 text-xs flex-shrink-0"
                      onClick={() => removeKvRow(i)}
                    >
                      ✕
                    </button>
                  </div>
                ))}
                <button
                  type="button"
                  className="text-xs text-brand hover:text-blue-400"
                  onClick={addKvRow}
                >
                  + Add field
                </button>
              </div>
            )}
          </div>

          <div className="flex gap-2 px-5 py-4 border-t border-gray-700 flex-shrink-0">
            <button
              type="submit"
              disabled={loading}
              className="flex-1 bg-green-950 border border-green-800 text-green-400 hover:bg-green-900 py-2 rounded text-sm font-semibold disabled:opacity-50 transition-colors"
            >
              {loading ? 'Saving…' : isEdit ? 'Update' : 'Insert'}
            </button>
            <button
              type="button"
              className="bg-gray-800 text-gray-400 hover:bg-gray-700 px-4 rounded text-sm transition-colors"
              onClick={onClose}
            >
              Cancel
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Verify insert and edit work**

With server running: open a collection, click "+ Insert", add a record in form mode, submit. Verify the record appears in the table. Click ✏ on a record, change a value, submit. Verify the change is reflected.

- [ ] **Step 3: Commit**

```bash
git add clients/web/src/components/RecordModal.tsx
git commit -m "feat(web): add RecordModal with JSON and form modes"
```

---

## Task 11: CollectionView tab router

**Files:**
- Modify: `clients/web/src/components/CollectionView.tsx`

- [ ] **Step 1: Rewrite `clients/web/src/components/CollectionView.tsx`**

```tsx
import { useState } from 'react'
import { BrowseTab } from './BrowseTab'
import { IndexesTab } from './IndexesTab'
import { StatsTab } from './StatsTab'
import { WatchTab } from './WatchTab'

type Tab = 'browse' | 'indexes' | 'stats' | 'watch'

interface Props { collection: string }

export function CollectionView({ collection }: Props) {
  const [tab, setTab] = useState<Tab>('browse')

  const tabs: { id: Tab; label: string }[] = [
    { id: 'browse', label: 'Browse' },
    { id: 'indexes', label: 'Indexes' },
    { id: 'stats', label: 'Stats' },
    { id: 'watch', label: '⚡ Watch' },
  ]

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center border-b border-gray-700 bg-gray-900 px-4 flex-shrink-0">
        {tabs.map((t) => (
          <button
            key={t.id}
            className={`px-4 py-2.5 text-sm transition-colors ${
              tab === t.id
                ? 'border-b-2 border-brand text-brand'
                : 'text-gray-500 hover:text-gray-300'
            }`}
            onClick={() => setTab(t.id)}
          >
            {t.label}
          </button>
        ))}
        <span className="ml-auto text-xs text-gray-600 pr-2 font-mono">{collection}</span>
      </div>

      <div className="flex-1 overflow-hidden">
        {tab === 'browse' && <BrowseTab collection={collection} />}
        {tab === 'indexes' && <IndexesTab collection={collection} />}
        {tab === 'stats' && <StatsTab collection={collection} />}
        {tab === 'watch' && <WatchTab collection={collection} />}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Create placeholder tab components so it compiles**

Create `clients/web/src/components/IndexesTab.tsx`:
```tsx
interface Props { collection: string }
export function IndexesTab({ collection }: Props) {
  return <div className="p-4 text-gray-400">IndexesTab: {collection}</div>
}
```

Create `clients/web/src/components/StatsTab.tsx`:
```tsx
interface Props { collection: string }
export function StatsTab({ collection }: Props) {
  return <div className="p-4 text-gray-400">StatsTab: {collection}</div>
}
```

Create `clients/web/src/components/WatchTab.tsx`:
```tsx
interface Props { collection: string }
export function WatchTab({ collection }: Props) {
  return <div className="p-4 text-gray-400">WatchTab: {collection}</div>
}
```

- [ ] **Step 3: Commit**

```bash
git add clients/web/src/components/CollectionView.tsx clients/web/src/components/IndexesTab.tsx clients/web/src/components/StatsTab.tsx clients/web/src/components/WatchTab.tsx
git commit -m "feat(web): add CollectionView tab router"
```

---

## Task 12: IndexesTab

**Files:**
- Modify: `clients/web/src/components/IndexesTab.tsx`

- [ ] **Step 1: Rewrite `clients/web/src/components/IndexesTab.tsx`**

```tsx
import { useCallback, useEffect, useState } from 'react'
import { useApp } from '../contexts/AppContext'
import { useToast } from '../contexts/ToastContext'

interface Props { collection: string }

export function IndexesTab({ collection }: Props) {
  const { client } = useApp()
  const { addToast } = useToast()
  const [indexes, setIndexes] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [newField, setNewField] = useState('')
  const [ensuring, setEnsuring] = useState(false)

  const refresh = useCallback(async () => {
    if (!client) return
    setLoading(true)
    try {
      setIndexes(await client.listIndexes(collection))
    } catch (err) {
      addToast('error', 'Failed to load indexes', (err as Error).message)
    } finally {
      setLoading(false)
    }
  }, [client, collection, addToast])

  useEffect(() => { refresh() }, [refresh])

  const handleEnsure = async (e: React.FormEvent) => {
    e.preventDefault()
    const field = newField.trim()
    if (!field || !client) return
    setEnsuring(true)
    try {
      await client.ensureIndex(collection, field)
      addToast('success', 'Index created', `${collection}.${field}`)
      setNewField('')
      await refresh()
    } catch (err) {
      addToast('error', 'Failed to create index', (err as Error).message)
    } finally {
      setEnsuring(false)
    }
  }

  const handleDrop = async (field: string) => {
    if (!client || !confirm(`Drop index on "${field}"?`)) return
    try {
      await client.dropIndex(collection, field)
      addToast('success', 'Index dropped', `${collection}.${field}`)
      await refresh()
    } catch (err) {
      addToast('error', 'Failed to drop index', (err as Error).message)
    }
  }

  return (
    <div className="p-6 max-w-lg">
      <div className="flex items-center justify-between mb-4">
        <span className="text-xs text-gray-500 uppercase tracking-wider">
          Secondary Indexes on <span className="text-brand font-mono">{collection}</span>
        </span>
        <form onSubmit={handleEnsure} className="flex gap-2 items-center">
          <input
            className="w-32 text-xs"
            placeholder="field name"
            value={newField}
            onChange={(e) => setNewField(e.target.value)}
            required
          />
          <button
            type="submit"
            disabled={ensuring}
            className="text-xs px-3 py-1 bg-green-950 border border-green-800 text-green-400 hover:bg-green-900 rounded disabled:opacity-50 transition-colors"
          >
            {ensuring ? '…' : '+ Ensure Index'}
          </button>
        </form>
      </div>

      {loading ? (
        <p className="text-xs text-gray-600">Loading…</p>
      ) : indexes.length === 0 ? (
        <div className="text-center py-8">
          <p className="text-gray-600 text-sm">No secondary indexes</p>
          <p className="text-gray-700 text-xs mt-1">
            Indexed fields accelerate eq-filter queries from O(n) → O(1)
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {indexes.map((field) => (
            <div
              key={field}
              className="flex items-center justify-between bg-gray-900 border border-gray-700 rounded-lg px-4 py-3"
            >
              <div className="flex items-center gap-3">
                <span className="text-green-400 text-sm">⬡</span>
                <span className="font-mono text-sm text-gray-200">{field}</span>
              </div>
              <button
                className="text-xs px-3 py-1 bg-red-950 border border-red-900 text-red-400 hover:bg-red-900 rounded transition-colors"
                onClick={() => handleDrop(field)}
              >
                Drop
              </button>
            </div>
          ))}
          <p className="text-xs text-gray-700 pt-2 px-1">
            Indexed fields accelerate eq-filter queries from O(n) → O(1)
          </p>
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add clients/web/src/components/IndexesTab.tsx
git commit -m "feat(web): implement IndexesTab"
```

---

## Task 13: StatsTab

**Files:**
- Modify: `clients/web/src/components/StatsTab.tsx`

- [ ] **Step 1: Rewrite `clients/web/src/components/StatsTab.tsx`**

```tsx
import { useCallback, useEffect, useRef, useState } from 'react'
import type { CollectionStats } from '../api/types'
import { useApp } from '../contexts/AppContext'
import { useToast } from '../contexts/ToastContext'

interface Props { collection: string }

function formatBytes(bytes: string): string {
  const n = Number(bytes)
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function dirtyPct(stats: CollectionStats): number {
  const total = Number(stats.record_count) + Number(stats.dirty_entries)
  if (total === 0) return 0
  return Math.round((Number(stats.dirty_entries) / total) * 100)
}

export function StatsTab({ collection }: Props) {
  const { client } = useApp()
  const { addToast } = useToast()
  const [stats, setStats] = useState<CollectionStats | null>(null)
  const [loading, setLoading] = useState(false)
  const [lastRefreshed, setLastRefreshed] = useState<Date | null>(null)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const refresh = useCallback(async () => {
    if (!client) return
    setLoading(true)
    try {
      setStats(await client.collectionStats(collection))
      setLastRefreshed(new Date())
    } catch (err) {
      addToast('error', 'Failed to load stats', (err as Error).message)
    } finally {
      setLoading(false)
    }
  }, [client, collection, addToast])

  useEffect(() => {
    refresh()
    intervalRef.current = setInterval(refresh, 30_000)
    return () => { if (intervalRef.current) clearInterval(intervalRef.current) }
  }, [refresh])

  const pct = stats ? dirtyPct(stats) : 0

  return (
    <div className="p-6 max-w-lg">
      <div className="flex items-center justify-between mb-6">
        <span className="text-xs text-gray-500 uppercase tracking-wider">
          Stats for <span className="text-brand font-mono">{collection}</span>
        </span>
        <div className="flex items-center gap-3 text-xs text-gray-600">
          {lastRefreshed && <span>Refreshed {lastRefreshed.toLocaleTimeString()}</span>}
          <button
            className="text-brand hover:text-blue-400 transition-colors"
            onClick={refresh}
            disabled={loading}
          >
            ↻ Refresh
          </button>
        </div>
      </div>

      {loading && !stats ? (
        <p className="text-xs text-gray-600">Loading…</p>
      ) : stats ? (
        <div className="grid grid-cols-2 gap-4">
          <div className="bg-gray-900 border border-gray-700 rounded-xl p-4">
            <div className="text-xs text-gray-500 uppercase tracking-wider mb-2">Records</div>
            <div className="text-3xl font-bold font-mono text-gray-100">{Number(stats.record_count).toLocaleString()}</div>
          </div>
          <div className="bg-gray-900 border border-gray-700 rounded-xl p-4">
            <div className="text-xs text-gray-500 uppercase tracking-wider mb-2">Segments</div>
            <div className="text-3xl font-bold font-mono text-gray-100">{stats.segment_count}</div>
          </div>
          <div className="bg-gray-900 border border-gray-700 rounded-xl p-4">
            <div className="text-xs text-gray-500 uppercase tracking-wider mb-2">Dirty Entries</div>
            <div className="flex items-baseline gap-2 mb-2">
              <div className={`text-3xl font-bold font-mono ${pct > 20 ? 'text-amber-400' : 'text-gray-100'}`}>
                {stats.dirty_entries}
              </div>
              <div className="text-xs text-gray-500">{pct}% stale</div>
            </div>
            <div className="bg-gray-800 rounded-full h-1.5">
              <div
                className={`h-1.5 rounded-full transition-all ${pct > 20 ? 'bg-amber-400' : 'bg-green-500'}`}
                style={{ width: `${Math.min(pct, 100)}%` }}
              />
            </div>
          </div>
          <div className="bg-gray-900 border border-gray-700 rounded-xl p-4">
            <div className="text-xs text-gray-500 uppercase tracking-wider mb-2">Size on Disk</div>
            <div className="text-3xl font-bold font-mono text-gray-100">{formatBytes(stats.size_bytes)}</div>
          </div>
        </div>
      ) : null}
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add clients/web/src/components/StatsTab.tsx
git commit -m "feat(web): implement StatsTab with auto-refresh"
```

---

## Task 14: WatchTab and useWatch hook

**Files:**
- Create: `clients/web/src/hooks/useWatch.ts`
- Modify: `clients/web/src/components/WatchTab.tsx`

- [ ] **Step 1: Create `clients/web/src/hooks/useWatch.ts`**

```typescript
import { useCallback, useRef, useState } from 'react'
import type { WatchEvent } from '../api/types'
import { useApp } from '../contexts/AppContext'
import { useToast } from '../contexts/ToastContext'

const MAX_EVENTS = 200

export function useWatch(collection: string) {
  const { client } = useApp()
  const { addToast } = useToast()
  const [events, setEvents] = useState<WatchEvent[]>([])
  const [watching, setWatching] = useState(false)
  const cancelRef = useRef<(() => void) | null>(null)

  const start = useCallback(() => {
    if (!client || watching) return
    setWatching(true)
    const cancel = client.watch(
      collection,
      (event) => {
        setEvents((prev) => {
          const next = [event, ...prev]
          return next.slice(0, MAX_EVENTS)
        })
      },
      (err) => {
        addToast('error', 'Watch disconnected', err.message)
        setWatching(false)
        cancelRef.current = null
      },
    )
    cancelRef.current = cancel
  }, [client, collection, watching, addToast])

  const stop = useCallback(() => {
    cancelRef.current?.()
    cancelRef.current = null
    setWatching(false)
  }, [])

  const clear = useCallback(() => setEvents([]), [])

  return { events, watching, start, stop, clear }
}
```

- [ ] **Step 2: Rewrite `clients/web/src/components/WatchTab.tsx`**

```tsx
import { useEffect } from 'react'
import { useWatch } from '../hooks/useWatch'
import type { WatchEvent } from '../api/types'

interface Props { collection: string }

const OP_STYLE: Record<string, string> = {
  INSERTED: 'bg-green-950 text-green-400 border-green-800',
  UPDATED: 'bg-blue-950 text-blue-400 border-blue-800',
  DELETED: 'bg-red-950 text-red-400 border-red-800',
}

function EventRow({ event }: { event: WatchEvent }) {
  const ts = event.ts ? new Date(event.ts).toLocaleTimeString() : '—'
  const dataStr = event.record?.data ? JSON.stringify(event.record.data) : '—'
  return (
    <div className="flex items-start gap-3 py-2 px-3 border-b border-gray-800 text-xs font-mono hover:bg-gray-800/30">
      <span className="text-gray-600 min-w-16 flex-shrink-0">{ts}</span>
      <span className={`px-2 py-0.5 rounded text-[10px] border min-w-16 text-center flex-shrink-0 ${OP_STYLE[event.op] ?? ''}`}>
        {event.op.replace('ED', '')}
      </span>
      <span className="text-brand flex-shrink-0">id:{event.record?.id ?? '—'}</span>
      <span className="text-gray-300 flex-1 truncate">{dataStr}</span>
    </div>
  )
}

export function WatchTab({ collection }: Props) {
  const { events, watching, start, stop, clear } = useWatch(collection)

  // Auto-start on mount
  useEffect(() => {
    start()
    return () => stop()
  }, [collection]) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-3 px-4 py-2 bg-gray-900 border-b border-gray-700 flex-shrink-0">
        <span className="text-xs text-gray-500 uppercase tracking-wider">Live Feed</span>
        {watching ? (
          <span className="px-2 py-0.5 rounded-full text-xs bg-green-950 text-green-400 border border-green-800 animate-pulse">
            ● watching
          </span>
        ) : (
          <span className="px-2 py-0.5 rounded-full text-xs bg-gray-800 text-gray-500 border border-gray-700">
            ○ stopped
          </span>
        )}
        <div className="ml-auto flex gap-2">
          {watching ? (
            <button
              className="text-xs px-3 py-1 bg-red-950 border border-red-900 text-red-400 hover:bg-red-900 rounded transition-colors"
              onClick={stop}
            >
              Stop
            </button>
          ) : (
            <button
              className="text-xs px-3 py-1 bg-green-950 border border-green-800 text-green-400 hover:bg-green-900 rounded transition-colors"
              onClick={start}
            >
              Start
            </button>
          )}
          <button
            className="text-xs px-3 py-1 bg-gray-800 border border-gray-700 text-gray-400 hover:bg-gray-700 rounded transition-colors"
            onClick={clear}
          >
            Clear
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto bg-gray-950">
        {events.length === 0 ? (
          <div className="flex items-center justify-center h-full text-gray-600 text-sm">
            {watching ? 'Waiting for events…' : 'Not watching. Click Start.'}
          </div>
        ) : (
          events.map((e, i) => <EventRow key={i} event={e} />)
        )}
      </div>

      <div className="px-4 py-1.5 bg-gray-900 border-t border-gray-700 text-xs text-gray-600 flex-shrink-0">
        {events.length} events · max {200} kept in memory
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Verify Watch tab works**

With server running: open a collection, click the "⚡ Watch" tab. The badge should show "● watching". In another terminal or the CLI, insert a record: `bin/filedb-cli insert users '{"name":"test"}'`. The INSERT event should appear in the live feed.

- [ ] **Step 4: Commit**

```bash
git add clients/web/src/hooks/useWatch.ts clients/web/src/components/WatchTab.tsx
git commit -m "feat(web): implement WatchTab with live event feed"
```

---

## Task 15: Documentation updates

**Files:**
- Modify: `docs/getting-started.md`
- Modify: `docs/architecture.md`
- Modify: `README.md`
- Modify: `ROADMAP.md`

- [ ] **Step 1: Add Web UI section to `docs/getting-started.md`**

Find the end of the file and append:

```markdown
## Web UI

FileDB includes a browser-based admin UI at `clients/web/`.

### Requirements

- Node.js 18+
- FileDB server running with CORS enabled (enabled by default)

### Development

```bash
cd clients/web
npm install
npm run dev      # opens http://localhost:5173
```

Open `http://localhost:5173`, enter your server URL (e.g. `http://localhost:8080`) and API key. The connection settings are saved to `localStorage`.

### Features

- **Browse** — filter, paginate, insert, edit, and delete records
- **Indexes** — create and drop secondary indexes per collection
- **Stats** — record count, segment count, dirty entries, and disk size (auto-refreshes every 30s)
- **Watch** — live change feed showing inserts, updates, and deletes in real time

### Production build

```bash
npm run build    # outputs to clients/web/dist/
```

The `dist/` directory can be served from any static file host. Make sure your FileDB server has CORS enabled (it is by default).
```

- [ ] **Step 2: Add web client section to `docs/architecture.md`**

Find the "## Network Layer" section and add after it:

```markdown
## Web UI Client

`clients/web/` is a standalone React + TypeScript + Vite SPA that connects to the REST gateway at `:8080`.

- **Talks to:** REST gateway only (not gRPC directly)
- **Auth:** sends `x-api-key` header on every request
- **Watch:** uses `fetch` with `ReadableStream` to consume the server-streaming Watch endpoint (`POST /v1/{collection}/watch`)
- **CORS:** the REST gateway wraps its handler with a permissive CORS middleware so browsers can call `:8080` from any origin

The web client has no server-side changes beyond the CORS middleware and the Watch RPC HTTP annotation added to `proto/filedb.proto`.
```

- [ ] **Step 3: Add web UI to `README.md` key properties list**

In `README.md`, find the key properties bullet list and add:

```markdown
- **Web UI** — browser-based admin interface at `clients/web/` for browsing collections, CRUD, indexes, stats, and live Watch feed
```

- [ ] **Step 4: Mark web UI item in `ROADMAP.md`**

In `ROADMAP.md`, find or add under "What Is Done ✅" a new item:

```markdown
### Web UI
- [x] `clients/web/` — React + TypeScript + Vite SPA
- [x] Browse tab: filter bar (field/op/value, AND/OR), record table with dynamic columns, pagination, insert/edit/delete
- [x] Indexes tab: list, ensure, drop secondary indexes
- [x] Stats tab: record count, segments, dirty entries, disk size with auto-refresh
- [x] Watch tab: live event feed via streaming Watch RPC
- [x] Connection settings persisted to localStorage
- [x] CORS middleware on REST gateway
```

- [ ] **Step 5: Commit**

```bash
git add docs/getting-started.md docs/architecture.md README.md ROADMAP.md
git commit -m "docs: add web UI documentation and update ROADMAP"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task |
|---|---|
| React + TypeScript + Vite | Task 2 |
| Sidebar with collection list + counts | Task 7 |
| Browse tab: filter bar (field/op/value, AND/OR, order, limit) | Task 8 + 9 |
| Browse tab: dynamic columns, expand row, edit/delete | Task 9 |
| Browse tab: insert/edit modal (JSON ↔ form) | Task 10 |
| Browse tab: pagination | Task 9 |
| Indexes tab: list, ensure, drop | Task 12 |
| Stats tab: 4 stat cards, auto-refresh | Task 13 |
| Watch tab: live feed, start/stop/clear | Task 14 |
| Create collection modal | Task 7 (inline in sidebar) |
| Drop collection with name confirmation | Task 7 (inline in sidebar) |
| Toast notifications (success/error/info, 4s auto-dismiss) | Task 4 |
| Connect screen + localStorage | Task 5 + 6 |
| Settings panel (URL + API key) | Task 6 |
| Connection status in top bar | Task 6 |
| API client (all REST endpoints) | Task 3 |
| API client unit tests | Task 3 |
| Watch REST annotation on server | Task 1 |
| CORS middleware on server | Task 1 |
| Docs updates | Task 15 |

All spec requirements covered. ✓

**Type consistency check:**
- `FileDBClient` defined in Task 3, used in Tasks 5, 7, 9, 12, 13, 14 — method names consistent (`deleteRecord`, `find`, `collectionStats`, etc.)
- `DBRecord`, `WatchEvent`, `CollectionStats`, `Filter`, `FindRequest` defined in `src/api/types.ts` (Task 3) and imported consistently across all components
- `useApp()` returns `client: FileDBClient | null` — all hooks guard with `if (!client) return`
- `FilterBarState` exported from `FilterBar.tsx` and imported in `BrowseTab.tsx`
- `filterBarStateToRequest` exported from `FilterBar.tsx` and imported in `BrowseTab.tsx`
