/** A record returned from FileDB. `id` is a string because uint64 exceeds JS's safe integer range. */
export interface DBRecord {
  id: string;
  data: Record<string, unknown>;
  date_added?: string;
  date_modified?: string;
}

/** An event emitted by the Watch RPC. */
export interface WatchEvent {
  op: 'INSERTED' | 'UPDATED' | 'DELETED';
  collection: string;
  record: DBRecord;
  ts?: string;
}

/** A filter on a single field. */
export interface FieldFilterInput {
  field: string;
  op: 'eq' | 'neq' | 'gt' | 'gte' | 'lt' | 'lte' | 'contains' | 'regex';
  value: string | number | boolean;
}

/** AND composite — all child filters must match. */
export interface AndFilterInput {
  and: FilterInput[];
}

/** OR composite — at least one child filter must match. */
export interface OrFilterInput {
  or: FilterInput[];
}

/** Union of all filter shapes accepted by `find` and `watch`. */
export type FilterInput = FieldFilterInput | AndFilterInput | OrFilterInput;

/** Options for the `find` / `findAll` methods. */
export interface FindOptions {
  filter?: FilterInput;
  /** Maximum number of results to return. 0 = no limit (default). */
  limit?: number;
  /** Number of leading results to skip. */
  offset?: number;
  /** Field name to sort by. */
  orderBy?: string;
  /** Sort descending when true. */
  descending?: boolean;
}

/** Collection statistics returned by `stats`. */
export interface StatsResult {
  collection: string;
  record_count: string;
  segment_count: string;
  dirty_entries: string;
  size_bytes: string;
}
