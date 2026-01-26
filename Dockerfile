# 使用官方 Go 镜像
FROM golang:1.21-alpine AS builder

# 安装必要的工具
RUN apk add --no-cache git

# 设置工作目录
WORKDIR /app

# 设置环境变量
ENV GOPROXY=https://goproxy.cn,direct
ENV GO111MODULE=on

# 复制Go模块文件
COPY go.mod go.sum ./

# 下载依赖（如果有）
RUN go mod download

# 复制源代码和静态文件
COPY main.go .
COPY index.html ./

# 编译应用（静态链接，减少依赖）
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o app .

# 使用最小化的运行时镜像
FROM alpine:latest

# 安装必要的运行时工具
RUN apk --no-cache add ca-certificates bash curl

# 设置工作目录
WORKDIR /root/

# 从构建阶段复制可执行文件和静态文件
COPY --from=builder /app/app .
COPY --from=builder /app/index.html ./

# 创建必要的目录
RUN mkdir -p /tmp

# 暴露端口
EXPOSE 7860

# 运行应用
CMD ["./app"]
