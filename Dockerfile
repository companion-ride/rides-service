FROM golang:1.25.7-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o main cmd/api/main.go

FROM alpine:3.20.1 AS prod
WORKDIR /app
COPY --from=build /app/main /app/main
COPY --from=build /app/migrations /app/migrations
COPY --from=build /app/web /app/web
COPY --from=build /app/docs /app/docs
EXPOSE ${PORT}
CMD ["./main"]


