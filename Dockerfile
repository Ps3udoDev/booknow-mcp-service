# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.27 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download && go mod verify

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api-server ./cmd/api-server

# ---- runtime ----
# Pin by digest in the release pipeline.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api-server /api-server
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api-server"]
