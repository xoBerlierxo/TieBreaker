# Security

## Reporting a vulnerability

Please report security issues privately via GitHub's **Report a vulnerability** button on the repository's Security tab, or by email to the maintainer listed on the GitHub profile. Please don't open a public issue.

## Security model (summary)

- **No user accounts.** A session is identified by a 128-bit random ID that acts like a password: anyone holding it can change that session. Share links (`/r/:shareId`) use a separate 128-bit ID and are read-only; they never reveal the session ID.
- **Admin API** (`/admin/v1/*`): HS256 JWTs, at most 1 hour long, with issuer and audience checked and `alg: none` rejected. Tokens are minted with `tiebreaker admin-token`, so whoever holds `JWT_SECRET` is an admin. Production refuses short secrets and the published development placeholders.
- **Abuse limits** (Redis, per client IP or session): session creation, `next-question` and simulation starts, plus a 1 MiB body limit. Behind a hosting proxy, set `CLIENT_IP_HEADER` so limits apply per real client.
- **Concurrency safety:** optimistic locking (ETag/If-Match), idempotency keys, and fenced job leases.
- **LLM:** off by default. Only car data and computed numbers are sent to Gemini; prompts and keys are never logged; model output is validated against the facts before use and rendered without raw HTML.
- **Data:** every catalogue value needs a source; unverified data is labelled in the API and UI.
- **Supply chain:** dependencies are pinned to releases at least two weeks old (except security fixes) and scanned with `govulncheck` and `npm audit`; CI actions are pinned to commit SHAs; the runtime image is distroless and runs as non-root.

## Running a check yourself

```bash
go install golang.org/x/vuln/cmd/govulncheck@v1.9.0 && govulncheck ./...
cd web && npm audit
golangci-lint run ./...     # includes gosec
```
