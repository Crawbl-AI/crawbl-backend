FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /usage-writer ./cmd/usage-writer

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /usage-writer /usr/local/bin/usage-writer
ENTRYPOINT ["usage-writer"]
