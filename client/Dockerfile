FROM golang:1.24 AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/client ./client/client.go

FROM scratch

WORKDIR /app

COPY --from=build /app/client .

CMD ["./client", "-log", "/app/logs/client.json"]