# The Go binary is built by CircleCI (architect/go-build) and attached to the
# build context as <binary>-<os>-<arch>; this image only assembles the runtime.
# For a local build, produce the binary first:
#   CGO_ENABLED=0 go build -o mcp-prometheus-linux-amd64 .
FROM gsoci.azurecr.io/giantswarm/alpine:3.20.3-giantswarm AS certs
FROM scratch

COPY --from=certs /etc/passwd /etc/passwd
COPY --from=certs /etc/group /etc/group
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ARG TARGETOS
ARG TARGETARCH
COPY mcp-prometheus-${TARGETOS}-${TARGETARCH} /mcp-prometheus
USER giantswarm

ENTRYPOINT ["/mcp-prometheus"]
CMD ["serve"]
