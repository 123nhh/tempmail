# ── 构建阶段 ─────────────────────────────────────────────
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
RUN npm run build

# ── 运行阶段 ─────────────────────────────────────────────
# Nginx 配置由 docker-compose 挂载 ../nginx/default.conf
FROM nginx:alpine
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
