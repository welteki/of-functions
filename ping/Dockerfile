FROM ghcr.io/openfaas/classic-watchdog:0.3.2 AS watchdog

FROM alpine:3.21.2

RUN mkdir -p /home/app

COPY --from=watchdog /fwatchdog /usr/bin/fwatchdog
RUN chmod +x /usr/bin/fwatchdog


# Populate example here - i.e. "cat", "sha512sum" or "node index.js"
ENV fprocess="ping -c 1 8.8.8.8"
# Set to true to see request in function logs
ENV write_debug="false"

EXPOSE 8080

HEALTHCHECK --interval=3s CMD [ -e /tmp/.lock ] || exit 1

CMD ["fwatchdog"]
