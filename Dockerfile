FROM golang:1.25.7-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

# Install swag CLI so the swagger spec is regenerated from current code on every build.
# Pinned to match the locally installed version; bump when upgrading.
RUN go install github.com/swaggo/swag/cmd/swag@v1.16.4

COPY . .

# Regenerate docs/swagger.json from current handler annotations.
RUN /go/bin/swag init \
    --dir cmd/api,internal/server \
    --output docs \
    --outputTypes json \
    --parseDependency --parseInternal

RUN go build -o main cmd/api/main.go

FROM alpine:3.20.1 AS prod
WORKDIR /app
COPY --from=build /app/main /app/main
COPY --from=build /app/migrations /app/migrations
COPY --from=build /app/web /app/web
COPY --from=build /app/docs /app/docs
EXPOSE ${PORT}
CMD ["./main"]


