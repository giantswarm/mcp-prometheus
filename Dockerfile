FROM alpine:3.19

# Curl is needed for the health probes
RUN apk --no-cache add ca-certificates curl

RUN addgroup -g 1000 -S appgroup && \
    adduser -u 1000 -S appuser -G appgroup

WORKDIR /

COPY mcp-prometheus .

USER appuser

EXPOSE 8080

ENTRYPOINT ["/mcp-prometheus"]
CMD ["serve"] 
