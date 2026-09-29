# Device token harvest sidecar.
# The base image ships Chromium + Xvfb matching playwright 1.63.0.
FROM mcr.microsoft.com/playwright:v1.63.0-noble

# Browsers are preinstalled in the base image; keep npm from re-downloading them.
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 \
    NODE_ENV=production \
    PORT=8090

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci --omit=dev && npm cache clean --force

COPY harvest.mjs docker-entrypoint.sh ./
RUN chmod +x docker-entrypoint.sh

EXPOSE 8090

# The entry point runs the command under xvfb-run (see docker-entrypoint.sh for
# why xvfb-run cannot be PID 1 in a container).
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["node", "harvest.mjs", "serve", "--port", "8090"]
