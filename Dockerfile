# syntax=docker/dockerfile:1
#
# OBIE container image (ADR 0017): static obied and obiectl on
# distroless/static, running as the unprivileged user nonroot (65532).
#
#   docker build -t obie .
#   docker run -d --name obie -v obie-state:/var/lib/obie -p 4001:4001 -p 4001:4001/udp obie
#   docker exec obie obiectl status
#
# The image runs with packaging/docker/obie.yaml; mount your own
# configuration over /etc/obie/obie.yaml. It uses the dryrun backend: the
# nftables backend needs the host's network namespace and CAP_NET_ADMIN
# (--network host --cap-add NET_ADMIN), which the systemd unit is the
# better fit for.
#
# The stage lab-init is the key and configuration generator of the
# compose lab in packaging/compose; the last stage is the image.

ARG GO_VERSION=1.26.7

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build
ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd cmd
COPY internal internal
COPY pkg pkg
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    for cmd in obied obiectl; do \
        CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
            -ldflags "-s -w -buildid= -X github.com/MNCloudwerksTechnology/obie/internal/version.Version=$VERSION" \
            -o /out/$cmd ./cmd/$cmd || exit 1; \
    done
# The directories obied needs, owned by nonroot: the state directory must
# not be writable by group or others.
RUN install -d -m 0700 /rootfs/var/lib/obie && install -d -m 0750 /rootfs/run/obie

FROM busybox:1.37 AS lab-init
COPY --from=build /out/obied /usr/local/bin/obied
COPY packaging/compose/lab-init.sh /usr/local/bin/lab-init
ENTRYPOINT ["/bin/sh", "/usr/local/bin/lab-init"]

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="OBIE" \
      org.opencontainers.image.description="Open Ban Intelligence Exchange node (obied, obiectl)" \
      org.opencontainers.image.source="https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="$VERSION"
COPY --from=build /out/obied /out/obiectl /usr/local/bin/
COPY --from=build --chown=65532:65532 --chmod=0700 /rootfs/var/lib/obie /var/lib/obie
COPY --from=build --chown=65532:65532 --chmod=0750 /rootfs/run/obie /run/obie
COPY packaging/docker/obie.yaml /etc/obie/obie.yaml
USER 65532:65532
VOLUME ["/var/lib/obie"]
# libp2p mesh (TCP and QUIC), metrics and health.
EXPOSE 4001/tcp 4001/udp 9464/tcp
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/usr/local/bin/obiectl", "--timeout", "4s", "status"]
ENTRYPOINT ["/usr/local/bin/obied"]
CMD ["--config", "/etc/obie/obie.yaml"]
