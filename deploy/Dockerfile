# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/trip-service ./cmd/trip-service

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/trip-service /trip-service
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/trip-service"]
