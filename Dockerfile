# syntax=docker/dockerfile:1

# 1) Build the Svelte UI -> internal/ui/dist
FROM node:22-slim AS ui
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build   # writes to /src/internal/ui/dist

# 2) Build the Go binary with the UI embedded
FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/internal/ui/dist ./internal/ui/dist
ARG BUILD_VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X github.com/yundera/mesh-console/internal/server.Version=${BUILD_VERSION}" \
      -o /mesh-console ./cmd/mesh-console

# 3) Runtime. The same image is also the host-verb runner (internal/dockerx
#    RunOnHost), which is the only reason nsenter is here: util-linux-misc
#    provides it. tzdata so the self-check log's local timestamps parse in the
#    host's TZ (passed in by the template).
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata util-linux-misc
COPY --from=backend /mesh-console /mesh-console
EXPOSE 8080
ENTRYPOINT ["/mesh-console"]
