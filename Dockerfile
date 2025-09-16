FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod ./

RUN go mod download

COPY . .

RUN go install github.com/swaggo/swag/cmd/swag@latest

RUN go mod tidy

RUN /go/bin/swag init --parseDependency --parseInternal -g cmd/server/main.go

RUN CGO_ENABLED=0 go build -o /app/server ./cmd/server

FROM alpine:latest

WORKDIR /

COPY --from=builder /app/server /server

COPY --from=builder /app/docs /docs

CMD ["/server"]