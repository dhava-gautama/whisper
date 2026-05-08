FROM golang:1.22-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -ldflags "-s -w" -o /bin/whisper .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -u 1000 whisper
WORKDIR /app
COPY --from=builder /bin/whisper .
COPY web/ ./web/
COPY users.json* ./
RUN mkdir -p /app/data/media && chown -R whisper:whisper /app/data
USER whisper
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["./whisper"]
