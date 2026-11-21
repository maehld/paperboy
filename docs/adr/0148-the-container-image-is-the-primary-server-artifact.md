# 0148: The container image is the primary server artifact

- Status: Accepted, implemented
- Date: 2026-11-21
- Builds on: ADR 0056 (release trains), ADR 0090 (image tags), ADR 0147 (separate releases)
- Amends: the Docker part of ADR 0147 (the image is a separate workflow)
- Answers: issue #370

## Context

ADR 0147 publishes the container image from a separate `docker.yml` workflow. The GitHub release and the image build ran beside each other, so a release could exist without its image, and a failed image did not block the release.

The image is how most installations run the server. A server release without a tested image is incomplete. The image is therefore the primary server artifact, not an addition to the binaries.

## Decision

### 1. The server release builds the image

`release.yml` gets a `docker` job that runs for `server-v*`. It builds and pushes the image with the tags of ADR 0147: `X.Y.Z` and, for a final, `X.Y` and `latest`. A prerelease gets only its full version.

The `release` job needs the image job. A failed image blocks the release, so a server release always names a pushed image.

### 2. The image leads the release

The server release notes start with the image reference. The binaries stay attached as secondary assets. The image itself stays in GHCR; a release asset is not an image registry.

### 3. The newest final server release is GitHub's latest

GitHub keeps one release as its latest. That is the newest final server release, because the server is the main product. A prerelease cannot be latest. A plugin or firmware release sets `make_latest: false`, so the badge stays with the server.

### 4. The Docker workflow only checks pull requests

`docker.yml` triggers on pull requests that touch the image inputs. It builds the image without pushing it. No `main` build and no plugin or firmware release publishes an image.

## Consequences

- A server release takes longer and fails when the image build fails.
- Plugin and firmware releases do not build the image.
- The image tags and the meaning of `latest` do not change ([ADR 0147](0147-separate-server-plugin-and-firmware-releases.md)).
- GitHub's latest names the newest final server release. A plugin or firmware release does not move it.
- The Go workspace writes `go.work.sum`. The build ignores it, so the binary is not stamped dirty and reports the tag version alone.
- A rollback of the image uses an older `X.Y.Z` tag; the binaries remain for installations without a container.
