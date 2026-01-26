# 第一阶段：构建Go应用
FROM golang:1.21-alpine AS builder

# 安装必要的构建工具
RUN apk add --no-cache git build-base

# 设置工作目录
WORKDIR /app

# 复制go.mod和go.sum文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o server main.go

# 第二阶段：创建轻量级运行镜像
FROM alpine:latest

# 安装必要的运行时依赖
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    wget \
    bash \
    && update-ca-certificates \
    && rm -rf /var/cache/apk/*

# 创建非root用户
RUN addgroup -g 1001 -S appuser && \
    adduser -u 1001 -S appuser -G appuser

# 创建应用目录
RUN mkdir -p /app /tmp/tmp && chown -R appuser:appuser /app /tmp/tmp

# 切换工作目录
WORKDIR /app

# 从构建阶段复制可执行文件
COPY --from=builder --chown=appuser:appuser /app/server /app/server

# 复制可能的静态文件（如index.html）
COPY --chown=appuser:appuser index.html /app/ 2>/dev/null || true

# 切换用户
USER appuser

# 暴露端口
EXPOSE 7860


# 启动应用
ENTRYPOINT ["/app/server"]
