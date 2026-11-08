# syntax=docker/dockerfile:1

# ---- build stage --------------------------------------------------------------
# Compiles a static Linux binary. CGO is off, so the binary needs no C libraries
# and runs on a minimal "distroless" image.
FROM golang:1.26.10-alpine AS build
WORKDIR /src

# Download modules first, in their own layer, so editing code doesn't
# re-download every dependency.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tiebreaker ./cmd/tiebreaker

# ---- runtime stage ------------------------------------------------------------
# distroless/static has no shell and no package manager: less to attack, and a
# tiny image. "nonroot" runs as an unprivileged user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tiebreaker /tiebreaker
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/tiebreaker"]
# No default CMD: pass a subcommand (serve | worker | migrate up | …) or set
# TIEBREAKER_MODE=serve|worker (used on Railway, one image for both services).
