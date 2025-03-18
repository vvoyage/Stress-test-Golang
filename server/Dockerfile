FROM golang:1.24 AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server ./server/server.go

FROM scratch

WORKDIR /app

COPY --from=build /app/server .

CMD ["./server", "-port", "8080", "-log", "/app/logs/server.json"]