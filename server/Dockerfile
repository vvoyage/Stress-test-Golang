FROM golang:1.24 AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server ./server/server.go

FROM alpine:latest

WORKDIR /app

COPY --from=build /app/server .

RUN mkdir -p /app/logs

EXPOSE 8080

CMD ["./server", "-port", "8080", "-log", "/app/logs/server.json"]