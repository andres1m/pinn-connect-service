FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

# Install swag tool to generate docs inside the container
RUN go install github.com/swaggo/swag/cmd/swag@latest

COPY . .

# Generate swagger docs (docs/ directory)
RUN swag init -g cmd/server/main.go

RUN CGO_ENABLED=0 GOOS=linux go build -o /pinn-server cmd/server/main.go

FROM alpine:latest

RUN apk add --no-cache ca-certificates docker-cli

COPY --from=builder /pinn-server /usr/local/bin/pinn-server

WORKDIR /app

COPY --from=builder /app/migrations ./migrations

EXPOSE 8080

ENTRYPOINT ["pinn-server"]
