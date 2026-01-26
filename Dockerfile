# 构建阶段：编译Go应用
FROM golang:1.21-alpine AS builder

# 安装必要的构建工具
RUN apk add --no-cache \
    git \
    ca-certificates \
    tzdata \
    && update-ca-certificates

# 设置工作目录
WORKDIR /app

# 复制go.mod和go.sum文件
COPY go.mod go.sum ./

# 下载Go模块依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建Go应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags='-w -s' -o server

# 最终运行时镜像
FROM alpine:3.18

# 安装必要的运行时工具
RUN apk add --no-cache \
    ca-certificates \
    bash \
    curl \
    wget \
    tzdata \
    libc6-compat \
    && rm -rf /var/cache/apk/*

# 创建非root用户
RUN addgroup -g 1001 -S appuser \
    && adduser -S appuser -u 1001

# 设置时区（可选）
ENV TZ=Asia/Shanghai

# 设置工作目录
WORKDIR /app

# 从构建阶段复制编译好的可执行文件
COPY --from=builder --chown=appuser:appuser /app/server ./
# 复制index.html（如果存在）
COPY --from=builder --chown=appuser:appuser /app/index.html ./

# 创建必要的目录并设置权限
RUN mkdir -p /app/tmp \
    && chown -R appuser:appuser /app \
    && chmod -R 755 /app

# 切换到非root用户
USER appuser

# 暴露端口
EXPOSE 7860 3000

# 运行应用
CMD ["./server"]
