FROM node:22.16.0-alpine AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY tsconfig.json vite.config.ts ./
COPY apps/console ./apps/console
COPY apps/player ./apps/player
ARG WEB_APP
RUN npm run check && npm run build:$WEB_APP && mv "apps/$WEB_APP/dist" /site

FROM nginx:1.28.0-alpine
ARG WEB_APP
ENV WEB_APP=$WEB_APP
ENV NGINX_ENVSUBST_FILTER="^MOCK_ENABLED$"
COPY --from=build /site /usr/share/nginx/html
COPY deploy/$WEB_APP.conf /etc/nginx/templates/default.conf.template
COPY --chmod=755 deploy/15-runtime.sh /docker-entrypoint.d/15-runtime.sh
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1/healthz"]
