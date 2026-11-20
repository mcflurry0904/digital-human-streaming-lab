FROM bluenviron/mediamtx:1.12.3 AS upstream
FROM alpine:3.22.0
COPY --from=upstream /mediamtx /usr/local/bin/mediamtx
COPY mediamtx.yml /etc/mediamtx.yml
ENTRYPOINT ["/usr/local/bin/mediamtx", "/etc/mediamtx.yml"]
