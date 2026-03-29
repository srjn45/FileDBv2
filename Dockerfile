FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

COPY filedb /usr/local/bin/filedb

RUN adduser -D -H -h /data filedb && \
    mkdir -p /data && \
    chown filedb:filedb /data

USER filedb
WORKDIR /data

EXPOSE 5433 8080

ENTRYPOINT ["filedb", "serve", "--data", "/data"]
