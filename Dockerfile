# 使用官方Go镜像进行构建
FROM golang:1.21-alpine AS builder

# 安装必要的构建工具
RUN apk add --no-cache git curl wget bash make gcc musl-dev ca-certificates

# 设置工作目录
WORKDIR /app

# 复制go.mod和go.sum文件
COPY go.mod go.sum ./

# 下载Go模块依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建Go应用
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o app main.go

# 创建必要的目录
RUN mkdir -p /app/tmp && chmod -R 755 /app/tmp

# 确保index.html存在（如果不存在则创建空文件）
RUN if [ ! -f index.html ]; then echo "Creating empty index.html" && echo "Hello world!" > index.html; fi

# 最终运行时镜像 - 使用更小的基础镜像
FROM alpine:3.18

# 安装必要的运行时工具
RUN apk add --no-cache \
    ca-certificates \
    bash \
    curl \
    wget \
    busybox-extras \
    libc6-compat \
    && rm -rf /var/cache/apk/*

# 创建非root用户
RUN addgroup -g 1001 -S appuser \
    && adduser -S appuser -u 1001 -G appuser

# 设置工作目录
WORKDIR /app

# 从构建阶段复制可执行文件和必要的文件
COPY --from=builder --chown=appuser:appuser /app/app ./
COPY --from=builder --chown=appuser:appuser /app/index.html ./

# 创建必要的目录并设置权限
RUN mkdir -p /app/tmp \
    && chown -R appuser:appuser /app \
    && chmod -R 755 /app \
    && chmod +x /app/app

# 切换到非root用户
USER appuser

# 暴露端口
EXPOSE 7860

# 健康检查
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD curl -f http://localhost:3000/ || exit 1

# 设置环境变量
ENV TZ=UTC

# 运行应用
CMD ["./app"]
