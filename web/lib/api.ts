// A thin, typed client for the Tiebreaker API.
//
// It handles the three HTTP conventions the backend relies on:
//   - the error shape { error: { code, message, details } }  → ApiError
//   - optimistic locking: the session version comes back as an ETag and must be
//     sent as If-Match on every change
//   - Idempotency-Key on answers: one key per logical submission, reused on retry

export const API_BASE = (process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080").replace(/\/$/, "");

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public details: Record<string, unknown> = {},
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export interface ApiResult<T> {
  data: T;
  etag?: string; // the session version, without quotes
  replayed: boolean; // true if the server returned a stored idempotent response
}

export interface RequestOptions {
  body?: unknown;
  ifMatch?: string;
  idempotencyKey?: string;
  fetchImpl?: typeof fetch; // injectable for tests
  base?: string;
}

/** request sends one API call and decodes the JSON response or error. */
export async function request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<ApiResult<T>> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  if (opts.ifMatch) headers["If-Match"] = `"${opts.ifMatch}"`;
  if (opts.idempotencyKey) headers["Idempotency-Key"] = opts.idempotencyKey;

  const res = await (opts.fetchImpl ?? fetch)(`${opts.base ?? API_BASE}${path}`, {
    method,
    headers,
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  });
  const text = await res.text();
  const json = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const err = json?.error ?? {};
    throw new ApiError(res.status, err.code ?? "HTTP_" + res.status, err.message ?? res.statusText, err.details ?? {});
  }
  return {
    data: json as T,
    etag: res.headers.get("ETag")?.replaceAll('"', "") ?? undefined,
    replayed: res.headers.get("Idempotent-Replayed") === "true",
  };
}

/** newIdempotencyKey creates a random key for one logical submission. */
export function newIdempotencyKey(): string {
  return crypto.randomUUID();
}

/**
 * submitIdempotent sends a POST that may be retried safely: on a network
 * failure it retries ONCE with the SAME Idempotency-Key, so the server either
 * stores the answer once or replays the stored response.
 */
export async function submitIdempotent<T>(path: string, body: unknown, ifMatch: string, opts: RequestOptions = {}): Promise<ApiResult<T>> {
  const key = opts.idempotencyKey ?? newIdempotencyKey();
  const send = () => request<T>("POST", path, { ...opts, body, ifMatch, idempotencyKey: key });
  try {
    return await send();
  } catch (e) {
    if (e instanceof ApiError) throw e; // the server answered: don't retry
    return await send(); // network error: retry with the same key
  }
}

/** isVersionConflict reports a stale If-Match (reload the session and retry). */
export function isVersionConflict(e: unknown): boolean {
  return e instanceof ApiError && e.code === "SESSION_VERSION_CONFLICT";
}

/** errorMessage turns any error into a sentence for the UI. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return String(e);
}
