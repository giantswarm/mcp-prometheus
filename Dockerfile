FROM --platform=$BUILDPLATFORM golang:1.26.2 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags "-w -extldflags '-static'" \
    -o mcp-prometheus .

FROM gsoci.azurecr.io/giantswarm/alpine:3.20.3-giantswarm AS certs
FROM scratch

COPY --from=certs /etc/passwd /etc/passwd
COPY --from=certs /etc/group /etc/group
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /app/mcp-prometheus /mcp-prometheus
USER giantswarm

ENTRYPOINT ["/mcp-prometheus"]
CMD ["serve"]
