# syntax=docker/dockerfile:1

# --- Go build stage ---
FROM golang:1.27-alpine AS go-build

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /pocketbase

# --- Final image ---
FROM alpine:latest

ARG APP_VERSION=dev
ENV APP_VERSION=$APP_VERSION

RUN apk add --no-cache ca-certificates exiftool

COPY --from=go-build /pocketbase /pb/pocketbase

COPY pb_migrations /pb/pb_migrations

WORKDIR /pb

EXPOSE 8090

CMD ["./pocketbase", "serve", "--http=0.0.0.0:8090"]