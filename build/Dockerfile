# Stage 1: Build
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Копируем зависимости
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем статический бинарный файл
RUN CGO_ENABLED=0 GOOS=linux go build -o /pinn-server cmd/server/main.go

# Stage 2: Runtime
FROM alpine:latest

# Устанавливаем необходимые пакеты
RUN apk add --no-cache ca-certificates docker-cli

# Копируем бинарный файл из builder
COPY --from=builder /pinn-server /usr/local/bin/pinn-server

# Настраиваем рабочую директорию
WORKDIR /app

# Сервер будет слушать на 8080 по умолчанию
EXPOSE 8080

ENTRYPOINT ["pinn-server"]
