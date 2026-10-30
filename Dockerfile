#   docker build -t paperboy .
#
# .dockerignore excludes .git, so the image has no VCS information to embed.
# Pass the release data explicitly to keep the dashboard header meaningful:
#   docker build \
#     --build-arg VERSION=$(git describe --tags --always) \
#     --build-arg COMMIT=$(git rev-parse HEAD) \
#     --build-arg BUILD_DATE=$(git log -1 --format=%cI) \
#     -t paperboy .
FROM golang:1.27-alpine AS build
ARG VERSION=dev
ARG COMMIT=
ARG BUILD_DATE=
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w \
      -X github.com/maehld/paperboy/internal/paperboy.buildVersionTag=${VERSION} \
      -X github.com/maehld/paperboy/internal/paperboy.buildCommit=${COMMIT} \
      -X github.com/maehld/paperboy/internal/paperboy.buildTime=${BUILD_DATE}" \
    -o /app ./cmd/paperboy

FROM alpine:3.22
RUN apk add --no-cache tzdata && mkdir /config && chown 65532:65532 /config
ENV TZ=Europe/Berlin
WORKDIR /config
COPY --from=build /app /usr/local/bin/paperboy
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/paperboy"]
