#   docker build -t paperboy .
#
# buildvcs stamps commit and date only when Git finds the repository, so .git
# stays in the build context (see .dockerignore) and the build stage installs
# the git binary; the alpine Go image does not ship one. The final image copies
# only the binary.
FROM golang:1.27-alpine AS build
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app ./cmd/paperboy

FROM alpine:3.22
RUN apk add --no-cache tzdata && mkdir /config && chown 65532:65532 /config
ENV TZ=Europe/Berlin
WORKDIR /config
COPY --from=build /app /usr/local/bin/paperboy
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/paperboy"]
