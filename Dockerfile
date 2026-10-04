# syntax=docker/dockerfile:1

# 阶段 1：构建内嵌前端（web/ → internal/ui/dist）
FROM --platform=$BUILDPLATFORM node:24-alpine AS webui-builder
WORKDIR /src
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci
COPY web/ ./web/
RUN cd web && npm run build

# 阶段 2：交叉编译静态二进制（ldflags 与 release.yml 一致；Flavor 留空，
# 跨架构构建时 runtime.GOARCH/GOARM 自然报告正确架构）
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=""
ARG COMMIT=""
ARG BUILT_AT=""
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=webui-builder /src/internal/ui/dist ./internal/ui/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags="-s -w \
      -X rosboard/internal/buildinfo.Version=${VERSION} \
      -X rosboard/internal/buildinfo.Commit=${COMMIT} \
      -X rosboard/internal/buildinfo.BuiltAt=${BUILT_AT}" \
    -o /out/rosboard ./cmd/rosboard

# 阶段 3：运行时镜像
FROM alpine:3.22
ARG VERSION=""
ARG COMMIT=""
ARG BUILT_AT=""
LABEL org.opencontainers.image.title="rosboard" \
      org.opencontainers.image.description="RouterOS monitoring and policy routing dashboard" \
      org.opencontainers.image.source="https://github.com/jasonxtt/rosboard" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILT_AT}"
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 1000 rosboard \
    && adduser -D -u 1000 -G rosboard rosboard \
    && mkdir -p /var/lib/rosboard \
    && chown rosboard:rosboard /var/lib/rosboard
# 容器内禁用在线自更新：升级 = 拉新镜像重建容器（见 docs/docker.md）；
# 不设 ROSBOARD_DATA_DIR，data_dir 默认 ./data 落在 WORKDIR 下，可由 config.yaml 覆盖
ENV ROSBOARD_UPDATE_DISABLED=1
WORKDIR /var/lib/rosboard
VOLUME ["/var/lib/rosboard"]
COPY --from=builder --chown=rosboard:rosboard /out/rosboard /usr/local/bin/rosboard
USER rosboard
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null "http://127.0.0.1:${ROSBOARD_PORT:-8080}/" || exit 1
ENTRYPOINT ["/usr/local/bin/rosboard"]
