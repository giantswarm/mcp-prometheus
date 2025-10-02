FROM gsoci.azurecr.io/giantswarm/alpine:3.20.3-giantswarm AS user-source

FROM scratch

COPY --from=user-source /etc/passwd /etc/passwd
COPY --from=user-source /etc/group /etc/group

ADD mcp-prometheus /
USER giantswarm

ENTRYPOINT ["/mcp-prometheus"]
CMD ["serve"]
