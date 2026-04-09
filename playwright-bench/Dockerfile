FROM ghcr.io/openfaas/classic-watchdog:0.3.2 AS watchdog

# Official Playwright image - includes Chromium, Firefox, WebKit and all
# required system dependencies pre-installed.
FROM mcr.microsoft.com/playwright:v1.52.0-noble

COPY --from=watchdog /fwatchdog /usr/bin/fwatchdog
RUN chmod +x /usr/bin/fwatchdog

WORKDIR /home/app

COPY package.json .

# PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 prevents the playwright npm package from
# trying to download browsers during install - they are already present in the
# base image at /ms-playwright.
RUN PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 npm install --omit=dev

COPY index.js .

# Classic watchdog forks `fprocess` per request and pipes stdin/stdout.
# Each invocation gets a fresh process, so Chromium launch time is measured
# on every request - which is exactly what this benchmark intends.
ENV fprocess="node index.js"
ENV mode="http"

EXPOSE 8080

HEALTHCHECK --interval=3s CMD [ -e /tmp/.lock ] || exit 1

CMD ["fwatchdog"]
