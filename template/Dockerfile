# {{Name}} in container mode. Build from this folder:
#   docker build -t {{name}} .
FROM infisical/cli:0.45.4 AS infisical

FROM node:24-slim
RUN apt-get update && apt-get install -y --no-install-recommends tini ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=infisical /bin/infisical /usr/local/bin/infisical
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund
COPY src src
COPY home/prompts home/prompts
COPY --chmod=755 entrypoint.sh entrypoint.sh
# home/memory is the writable part of the home; mount a volume here.
RUN mkdir -p home/memory && chown node:node home/memory
USER node
ENV {{NAME}}_HEADLESS=1 NODE_USE_ENV_PROXY=1
EXPOSE 8080
ENTRYPOINT ["/usr/bin/tini", "--", "/app/entrypoint.sh"]
