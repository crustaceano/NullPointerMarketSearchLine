# syntax=docker/dockerfile:1

FROM golang:1.26-bookworm AS build

WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/backend .

FROM debian:bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    chromium \
    fonts-dejavu \
    fonts-liberation \
    fonts-noto-color-emoji \
  && rm -rf /var/lib/apt/lists/*

RUN test -x /usr/bin/chromium

ENV ADDR=:8080 \
    ML_URL=http://ml:8000 \
    WEB_DIR=/app/web \
    ROD_BROWSER_BIN=/usr/bin/chromium \
    CHROME_BIN=/usr/bin/chromium

WORKDIR /app

COPY --from=build /out/backend /app/backend
COPY backend/web /app/web

EXPOSE 8080

CMD ["/app/backend"]
