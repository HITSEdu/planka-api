FROM golang:1.22.4-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/planka-api ./cmd/api

FROM alpine:3.20

RUN addgroup -S app && adduser -S app -G app

WORKDIR /app
COPY --from=build /out/planka-api /app/planka-api

USER app

EXPOSE 8080

ENTRYPOINT ["/app/planka-api"]
