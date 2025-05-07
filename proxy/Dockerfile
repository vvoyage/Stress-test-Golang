FROM golang:1.24 AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/proxy ./proxy/proxy.go

FROM scratch

WORKDIR /app

COPY --from=build /app/proxy .

CMD ["./proxy", "-log", "/app/logs/proxy.json"]