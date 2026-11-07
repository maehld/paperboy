#   docker build -t paperboy .
#
# buildvcs stamps commit and date only when Git finds the repository, so .git
# stays in the build context (see .dockerignore) and the build stage installs
# the git binary; the alpine Go image does not ship one. The final image copies
# only the binary. Plugins are not part of the image: they come as a zip per
# platform from the plugins workflow and are unpacked into the instance's
# plugins/ directory (ADR 0049).
FROM golang:1.27-alpine AS build
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The release tag reaches the binary only through VERSION: Go stamps a working
# tree build with a pseudo-version, never with the tag (ADR 0056).
ARG VERSION=
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/maehld/paperboy/internal/paperboy.version=${VERSION}" \
    -o /out/paperboy ./cmd/paperboy

FROM alpine:3.22
RUN apk add --no-cache tzdata && mkdir /config && chown 65532:65532 /config
ENV TZ=Europe/Berlin
WORKDIR /config
COPY --from=build /out/ /usr/local/bin/
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/paperboy"]
