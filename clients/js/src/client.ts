import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import * as fs from 'fs';
import * as path from 'path';
import {
  DBRecord,
  FieldFilterInput,
  FilterInput,
  FindOptions,
  StatsResult,
  WatchEvent,
} from './types';

// ---------------------------------------------------------------------------
// Proto loading — done once at module load time
// ---------------------------------------------------------------------------

const PROTO_PATH = path.join(__dirname, '..', 'proto', 'filedb.proto');
const INCLUDE_DIR = path.join(__dirname, '..', 'proto');

const packageDefinition = protoLoader.loadSync(PROTO_PATH, {
  keepCase: true,
  longs: String,
  enums: String,
  defaults: true,
  oneofs: true,
  includeDirs: [INCLUDE_DIR],
});

const protoDescriptor = grpc.loadPackageDefinition(packageDefinition) as any;
const FileDBStub = protoDescriptor.filedb.v1.FileDB as grpc.ServiceClientConstructor;

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

/** Wrap a unary gRPC callback method into a Promise. */
function callUnary<Req, Res>(
  fn: (
    req: Req,
    metadata: grpc.Metadata,
    callback: (err: grpc.ServiceError | null, res: Res) => void,
  ) => void,
  req: Req,
  metadata: grpc.Metadata,
): Promise<Res> {
  return new Promise((resolve, reject) => {
    fn(req, metadata, (err, res) => {
      if (err) return reject(err);
      resolve(res as Res);
    });
  });
}

const OP_MAP: Record<string, string> = {
  eq: 'EQ',
  neq: 'NEQ',
  gt: 'GT',
  gte: 'GTE',
  lt: 'LT',
  lte: 'LTE',
  contains: 'CONTAINS',
  regex: 'REGEX',
};

/** Convert a FilterInput plain object to the proto Filter wire format. */
function filterToProto(filter: FilterInput): object {
  if ('and' in filter) {
    return { kind: 'and', and: { filters: filter.and.map(filterToProto) } };
  }
  if ('or' in filter) {
    return { kind: 'or', or: { filters: filter.or.map(filterToProto) } };
  }
  const f = filter as FieldFilterInput;
  return {
    kind: 'field',
    field: {
      field: f.field,
      op: OP_MAP[f.op] ?? 'FILTER_OP_UNSPECIFIED',
      value: String(f.value),
    },
  };
}

/** Convert a raw proto Record object to a typed DBRecord. */
function toRecord(raw: any): DBRecord {
  return {
    id: String(raw.id),
    data: (raw.data as Record<string, unknown>) ?? {},
    date_added: raw.date_added ? String(raw.date_added) : undefined,
    date_modified: raw.date_modified ? String(raw.date_modified) : undefined,
  };
}

// ---------------------------------------------------------------------------
// FileDB client
// ---------------------------------------------------------------------------

/**
 * TypeScript/JavaScript client for FileDB v2.
 *
 * @example
 * ```ts
 * const db = new FileDB('localhost', 5433, 'dev-key');
 *
 * await db.createCollection('users');
 * const id = await db.insert('users', { name: 'Alice', age: 30 });
 * const record = await db.findById('users', id);
 * await db.update('users', id, { name: 'Alice', age: 31 });
 * await db.delete('users', id);
 * await db.dropCollection('users');
 * db.close();
 * ```
 */
export class FileDB {
  private readonly stub: grpc.Client & Record<string, Function>;
  private readonly apiKey: string;

  /**
   * Connect to a FileDB server.
   *
   * @param host       gRPC host (e.g. `'localhost'`)
   * @param port       gRPC port (default `5433`)
   * @param apiKey     API key — sent as `x-api-key` metadata on every call
   * @param tlsCaCert  Optional PEM buffer for TLS CA certificate verification.
   *                   Omit for a plaintext (insecure) connection.
   */
  constructor(host: string, port: number, apiKey: string, tlsCaCert?: Buffer) {
    this.apiKey = apiKey;
    const credentials = tlsCaCert
      ? grpc.credentials.createSsl(tlsCaCert)
      : grpc.credentials.createInsecure();
    this.stub = new FileDBStub(`${host}:${port}`, credentials) as any;
  }

  /**
   * Connect using a TLS CA certificate loaded from a file path.
   *
   * @param host           gRPC host
   * @param port           gRPC port
   * @param apiKey         API key
   * @param tlsCaCertPath  Path to PEM CA certificate file
   */
  static fromTlsCertPath(
    host: string,
    port: number,
    apiKey: string,
    tlsCaCertPath: string,
  ): FileDB {
    return new FileDB(host, port, apiKey, fs.readFileSync(tlsCaCertPath));
  }

  private meta(): grpc.Metadata {
    const md = new grpc.Metadata();
    md.add('x-api-key', this.apiKey);
    return md;
  }

  // -------------------------------------------------------------------------
  // Collection management
  // -------------------------------------------------------------------------

  /** Create a new collection. Returns the collection name. */
  async createCollection(name: string): Promise<string> {
    const resp: any = await callUnary(
      this.stub.CreateCollection.bind(this.stub),
      { name },
      this.meta(),
    );
    return resp.name as string;
  }

  /** Drop a collection and all its data. Returns `true` on success. */
  async dropCollection(name: string): Promise<boolean> {
    const resp: any = await callUnary(
      this.stub.DropCollection.bind(this.stub),
      { name },
      this.meta(),
    );
    return resp.ok as boolean;
  }

  /** List all collection names. */
  async listCollections(): Promise<string[]> {
    const resp: any = await callUnary(
      this.stub.ListCollections.bind(this.stub),
      {},
      this.meta(),
    );
    return (resp.names as string[]) ?? [];
  }

  // -------------------------------------------------------------------------
  // CRUD
  // -------------------------------------------------------------------------

  /** Insert one record. Returns the assigned ID as a string. */
  async insert(collection: string, data: Record<string, unknown>): Promise<string> {
    const resp: any = await callUnary(
      this.stub.Insert.bind(this.stub),
      { collection, data },
      this.meta(),
    );
    return String(resp.id);
  }

  /** Insert multiple records. Returns an array of assigned IDs in insertion order. */
  async insertMany(
    collection: string,
    records: Array<Record<string, unknown>>,
  ): Promise<string[]> {
    const resp: any = await callUnary(
      this.stub.InsertMany.bind(this.stub),
      { collection, records },
      this.meta(),
    );
    return ((resp.ids as unknown[]) ?? []).map(String);
  }

  /** Fetch a single record by ID. */
  async findById(collection: string, id: string | number): Promise<DBRecord> {
    const resp: any = await callUnary(
      this.stub.FindById.bind(this.stub),
      { collection, id: String(id) },
      this.meta(),
    );
    return toRecord(resp.record);
  }

  /**
   * Stream records matching the given options.
   * Returns an `AsyncGenerator` — use `for await` to iterate.
   *
   * @example
   * ```ts
   * for await (const record of db.find('users', { filter: { field: 'role', op: 'eq', value: 'admin' } })) {
   *   console.log(record);
   * }
   * ```
   */
  async *find(collection: string, opts: FindOptions = {}): AsyncGenerator<DBRecord> {
    const call = this.stub.Find(
      {
        collection,
        filter: opts.filter ? filterToProto(opts.filter) : undefined,
        limit: opts.limit ?? 0,
        offset: opts.offset ?? 0,
        order_by: opts.orderBy ?? '',
        descending: opts.descending ?? false,
      },
      this.meta(),
    ) as grpc.ClientReadableStream<any>;

    for await (const resp of call) {
      yield toRecord(resp.record);
    }
  }

  /**
   * Collect all results from `find` into an array.
   * Convenience wrapper for non-streaming use cases.
   */
  async findAll(collection: string, opts: FindOptions = {}): Promise<DBRecord[]> {
    const results: DBRecord[] = [];
    for await (const record of this.find(collection, opts)) {
      results.push(record);
    }
    return results;
  }

  /** Update a record by ID. Returns the updated ID. */
  async update(
    collection: string,
    id: string | number,
    data: Record<string, unknown>,
  ): Promise<string> {
    const resp: any = await callUnary(
      this.stub.Update.bind(this.stub),
      { collection, id: String(id), data },
      this.meta(),
    );
    return String(resp.id);
  }

  /** Delete a record by ID. Returns `true` if the record existed. */
  async delete(collection: string, id: string | number): Promise<boolean> {
    const resp: any = await callUnary(
      this.stub.Delete.bind(this.stub),
      { collection, id: String(id) },
      this.meta(),
    );
    return resp.ok as boolean;
  }

  // -------------------------------------------------------------------------
  // Secondary indexes
  // -------------------------------------------------------------------------

  /** Create a secondary index on a field (no-op if already exists). */
  async ensureIndex(collection: string, field: string): Promise<void> {
    await callUnary(
      this.stub.EnsureIndex.bind(this.stub),
      { collection, field },
      this.meta(),
    );
  }

  /** Drop a secondary index. Returns `true` if the index existed. */
  async dropIndex(collection: string, field: string): Promise<boolean> {
    const resp: any = await callUnary(
      this.stub.DropIndex.bind(this.stub),
      { collection, field },
      this.meta(),
    );
    return resp.ok as boolean;
  }

  /** List all indexed field names for a collection. */
  async listIndexes(collection: string): Promise<string[]> {
    const resp: any = await callUnary(
      this.stub.ListIndexes.bind(this.stub),
      { collection },
      this.meta(),
    );
    return (resp.fields as string[]) ?? [];
  }

  // -------------------------------------------------------------------------
  // Transactions
  // -------------------------------------------------------------------------

  /** Begin a transaction on a collection. Returns the transaction ID. */
  async beginTx(collection: string): Promise<string> {
    const resp: any = await callUnary(
      this.stub.BeginTx.bind(this.stub),
      { collection },
      this.meta(),
    );
    return resp.tx_id as string;
  }

  /** Commit a transaction. Returns `true` on success. */
  async commitTx(txId: string): Promise<boolean> {
    const resp: any = await callUnary(
      this.stub.CommitTx.bind(this.stub),
      { tx_id: txId },
      this.meta(),
    );
    return resp.ok as boolean;
  }

  /** Roll back a transaction. Returns `true` on success. */
  async rollbackTx(txId: string): Promise<boolean> {
    const resp: any = await callUnary(
      this.stub.RollbackTx.bind(this.stub),
      { tx_id: txId },
      this.meta(),
    );
    return resp.ok as boolean;
  }

  // -------------------------------------------------------------------------
  // Watch (server-streaming change feed)
  // -------------------------------------------------------------------------

  /**
   * Subscribe to change events on a collection.
   * Returns an `AsyncGenerator` — use `for await` to receive events.
   *
   * @example
   * ```ts
   * for await (const event of db.watch('users')) {
   *   console.log(event.op, event.record.id, event.record.data);
   * }
   * ```
   *
   * Break out of the loop to stop watching.
   */
  async *watch(collection: string, filter?: FilterInput): AsyncGenerator<WatchEvent> {
    const call = this.stub.Watch(
      {
        collection,
        filter: filter ? filterToProto(filter) : undefined,
      },
      this.meta(),
    ) as grpc.ClientReadableStream<any>;

    for await (const event of call) {
      yield {
        op: event.op as WatchEvent['op'],
        collection: event.collection as string,
        record: toRecord(event.record),
        ts: event.ts ? String(event.ts) : undefined,
      };
    }
  }

  // -------------------------------------------------------------------------
  // Stats
  // -------------------------------------------------------------------------

  /** Return collection statistics. */
  async stats(collection: string): Promise<StatsResult> {
    const resp: any = await callUnary(
      this.stub.CollectionStats.bind(this.stub),
      { collection },
      this.meta(),
    );
    return {
      collection: resp.collection as string,
      record_count: String(resp.record_count),
      segment_count: String(resp.segment_count),
      dirty_entries: String(resp.dirty_entries),
      size_bytes: String(resp.size_bytes),
    };
  }

  // -------------------------------------------------------------------------
  // Lifecycle
  // -------------------------------------------------------------------------

  /** Close the underlying gRPC channel. */
  close(): void {
    this.stub.close();
  }
}
