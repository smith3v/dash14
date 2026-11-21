FROM alpine:3.24

RUN apk add --no-cache flock sqlite yq
COPY backup.sh /usr/local/bin/dash14-backup
COPY schedule.sh /usr/local/bin/dash14-backup-schedule

ENTRYPOINT ["/bin/sh", "/usr/local/bin/dash14-backup-schedule"]
