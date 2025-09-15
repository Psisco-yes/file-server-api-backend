FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o /app/server ./cmd/server

FROM alpine:latest

WORKDIR /

COPY --from=builder /app/server /server

CMD ["/server"]