# Multi-stage: build with the official Go toolchain, ship a distroless or
# scratch image.
#
#   docker build -t ndpm .
#   docker run -v ./plugins:/data/plugins -e GITHUB_TOKEN=xxx \
#     ndpm --config /data/plugins/config.yaml
#
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO disabled -> static, portable binaries (also required mentally for
# cross-compilation in CI).
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ndpm .

FROM scratch
# CA certificates so the Go binary can verify GitHub's TLS. Copied from the
# alpine build stage (the static-compatible bundle is the right size).
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/ndpm /ndpm
# The plugins directory is a mount point; create it so it exists by default.
COPY --from=build /src/config.example.yaml /etc/ndpm/config.example.yaml
VOLUME ["/data/plugins"]
ENTRYPOINT ["/ndpm"]
CMD ["--config", "/etc/ndpm/config.example.yaml"]
