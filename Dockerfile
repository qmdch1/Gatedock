FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/ ./web/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sshdesk ./cmd/sshdesk

FROM alpine:3.24
RUN apk add --no-cache ca-certificates \
    && addgroup -g 1000 sshdesk \
    && adduser -D -u 1000 -G sshdesk sshdesk \
    && mkdir -p /data /home/sshdesk/.ssh \
    && chown -R sshdesk:sshdesk /data /home/sshdesk
COPY --from=build /out/sshdesk /usr/local/bin/sshdesk
COPY THIRD_PARTY_NOTICES.md /usr/share/doc/sshdesk/THIRD_PARTY_NOTICES.md
COPY licenses/ /usr/share/doc/sshdesk/licenses/
LABEL org.opencontainers.image.source="https://github.com/qmdch1/Gatedock"
USER 1000:1000
ENV HOME=/home/sshdesk
WORKDIR /data
STOPSIGNAL SIGINT
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
    CMD wget -q -O /dev/null http://127.0.0.1:9876/health || exit 1
ENTRYPOINT ["/usr/local/bin/sshdesk"]
CMD ["-no-browser", "-data-dir", "/data"]
