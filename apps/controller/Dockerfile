FROM golang:1.24.4-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -o /controller .

FROM alpine:3.22.0
RUN apk add --no-cache 'ffmpeg~6.1' && ffprobe -version >/dev/null
RUN mkdir -p /media && chown 10001:10001 /media
COPY --from=build /controller /usr/local/bin/controller
USER 10001:10001
ENV HTTP_ADDR=:8080
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/healthz"]
ENTRYPOINT ["/usr/local/bin/controller"]
