# syntax=docker/dockerfile:1

# --- SPA build (bun) ---
FROM oven/bun:1 AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ .
RUN bun run build

# --- Go build ---
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/ach .

# --- Runtime ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
ENV PATH="/app:${PATH}"
WORKDIR /app
COPY --from=build /out/ach /app/ach
COPY --from=web /src/web/dist /app/web/dist
EXPOSE 8090
CMD ["ach", "serve"]
