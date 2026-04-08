FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /pricing-refresh ./cmd/crawbl/platform/pricing-refresh

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /pricing-refresh /usr/local/bin/pricing-refresh
ENTRYPOINT ["pricing-refresh"]
