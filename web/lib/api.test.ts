import { describe, expect, it } from "vitest";
import { ApiError, isVersionConflict, request, submitIdempotent } from "./api";

type Call = { url: string; init: RequestInit };

function fakeFetch(responses: Array<Response | Error>): { fetchImpl: typeof fetch; calls: Call[] } {
  const calls: Call[] = [];
  const fetchImpl = (async (url: string, init: RequestInit) => {
    calls.push({ url, init });
    const next = responses.shift();
    if (next instanceof Error) throw next;
    return next!;
  }) as unknown as typeof fetch;
  return { fetchImpl, calls };
}

const json = (body: unknown, status = 200, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ...headers } });

describe("request", () => {
  it("sends If-Match quoted and reads the ETag without quotes", async () => {
    const { fetchImpl, calls } = fakeFetch([json({ id: "s1" }, 200, { ETag: '"4"' })]);
    const res = await request("PUT", "/v1/x", { body: { a: 1 }, ifMatch: "3", fetchImpl, base: "http://api" });
    const headers = calls[0].init.headers as Record<string, string>;
    expect(calls[0].url).toBe("http://api/v1/x");
    expect(headers["If-Match"]).toBe('"3"');
    expect(res.etag).toBe("4");
  });

  it("parses the error shape into ApiError", async () => {
    const { fetchImpl } = fakeFetch([
      json({ error: { code: "SESSION_VERSION_CONFLICT", message: "changed", details: { current_version: 7 } } }, 409),
    ]);
    const err = await request("POST", "/v1/x", { fetchImpl, base: "" }).catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(409);
    expect(err.details.current_version).toBe(7);
    expect(isVersionConflict(err)).toBe(true);
  });
});

describe("submitIdempotent", () => {
  it("reuses the same Idempotency-Key when retrying after a network error", async () => {
    const { fetchImpl, calls } = fakeFetch([new TypeError("network down"), json({ accepted: true }, 201, { "Idempotent-Replayed": "true" })]);
    const res = await submitIdempotent("/v1/answers", { q: 1 }, "2", { fetchImpl, base: "" });
    const k1 = (calls[0].init.headers as Record<string, string>)["Idempotency-Key"];
    const k2 = (calls[1].init.headers as Record<string, string>)["Idempotency-Key"];
    expect(calls).toHaveLength(2);
    expect(k1).toBeTruthy();
    expect(k2).toBe(k1);
    expect(res.replayed).toBe(true);
  });

  it("does not retry when the server answered with an error", async () => {
    const { fetchImpl, calls } = fakeFetch([json({ error: { code: "VALIDATION_FAILED", message: "bad" } }, 400)]);
    await expect(submitIdempotent("/v1/answers", {}, "1", { fetchImpl, base: "" })).rejects.toBeInstanceOf(ApiError);
    expect(calls).toHaveLength(1);
  });
});
