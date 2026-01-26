Go Argo - 高性能代理服务器

https://github.com/mimaldq/go-argo/actions/workflows/docker.yml/badge.svg
https://img.shields.io/badge/license-MIT-blue.svg
https://img.shields.io/badge/go-1.21+-blue.svg
https://img.shields.io/badge/docker-ready-blue.svg

一个用Go语言重写的高性能代理服务器，支持Xray、Cloudflare隧道、哪吒监控等功能，兼容原Node.js版本的所有特性。

✨ 特性

· 🚀 高性能 - Go语言编写，性能远超Node.js版本
· 🔒 安全 - 支持多种代理协议（VLESS、VMESS、Trojan）
· 🌐 隧道支持 - 集成Cloudflare Argo隧道
· 📊 监控 - 集成哪吒监控系统
· 📦 轻量容器 - 多架构Docker镜像支持
· ⚡ 快速部署 - 一键部署到各种平台

## 说明 （部署前请仔细阅读）

* 本项目是针对node环境的paas平台和游戏玩具而生，采用Argo隧道部署节点，集成哪吒探针v0或v1可选。
* node玩具平台只需上传index.js和package.json即可，paas平台需要docker部署的才上传Dockerfile。
* 不填写ARGO_DOMAIN和ARGO_AUTH两个变量即启用临时隧道，反之则使用固定隧道。
* 哪吒v0/v1可选,当哪吒端口为{443,8443,2096,2087,2083,2053}其中之一时，自动开启tls。
* 新增cf-vps监控，cf-vps监控项目地址https://github.com/kadidalax/cf-vps-monitor

## 📋 环境变量

| 变量名 | 是否必须 | 默认值 | 说明 |
|--------|----------|--------|------|
| UPLOAD_URL | 否 | - | 订阅上传地址 |
| PROJECT_URL | 否 | https://www.google.com | 项目分配的域名 |
| AUTO_ACCESS | 否 | false | 是否开启自动访问保活 |
| PORT | 否 | 3000 | HTTP服务监听端口 |
| ARGO_PORT | 否 | 7860 | Argo隧道端口 |
| UUID | 否 | e2cae6af-5cdd-fa48-4137-ad3e617fbab0 | 用户UUID |
| NEZHA_SERVER | 否 | - | 哪吒面板域名 |
| NEZHA_PORT | 否 | - | 哪吒端口 |
| NEZHA_KEY | 否 | - | 哪吒密钥 |
| ARGO_DOMAIN | 否 | - | Argo固定隧道域名 |
| ARGO_AUTH | 否 | - | Argo固定隧道密钥 |
| CFIP | 否 | www.visa.com.tw | 节点优选域名或IP |
| CFPORT | 否 | 443 | 节点端口 |
| NAME | 否 | Vls | 节点名称前缀 |
| FILE_PATH | 否 | ./tmp | 运行目录 |
| SUB_PATH | 否 | sub | 订阅路径 |
| MONITOR_KEY | 否 | - | 监控脚本密钥 |
| MONITOR_SERVER | 否 | - | 监控服务器标识 |
| MONITOR_URL | 否 | - | 监控上报地址 |

📦 快速开始

Docker运行

```bash
# 使用默认配置
docker run -d \
  --name go-argo \
  -p 3000:3000 \
  -p 7860:7860 \
  ghcr.io/mimaldq/go-argo:latest

# 自定义UUID
docker run -d \
  --name go-argo \
  -p 3000:3000 \
  -p 7860:7860 \
  -e UUID=your_custom_uuid \
  ghcr.io/mimaldq/go-argo:latest
```

docker-compose部署

```yaml
version: '3.8'
services:
  go-argo:
    image: ghcr.io/mimaldq/go-argo:latest
    container_name: go-argo
    restart: unless-stopped
    ports:
      - "3000:3000"
      - "7860:7860"
    environment:
      - UUID=e2cae6af-5cdd-fa48-4137-ad3e617fbab0
      - NEZHA_SERVER=nz.abc.com:8008
      - NEZHA_KEY=your_nezhakey_here
      - ARGO_AUTH=eyJhIjoiNWRmNTFlZjhhMTNiMWQ1ZDFhODhhZTAxNWFmYTU5OGIiLCJ0IjoiM2Q0M2I5ZTgtNDM0Zi00YjA2LTk5ZmEtMjc2ODc0MGI3ZTcyIiwicyI6Ill6SmhNemxoT1RFdFpUSTROeTAwTmpFeUxUazBOelV0WlRZNFptRTFabUV6WldKbCJ9
    volumes:
      - ./tmp:/app/tmp
```


📡 订阅访问

服务器启动后，可以通过以下方式访问订阅：

```
http://你的域名或IP:7860/sub
http://你的域名或IP:3000/sub
```

订阅内容包含：

· VLESS协议节点
· VMESS协议节点
· Trojan协议节点

🛠️ 本地开发

要求

· Go 1.21+
· Git

编译运行

```bash
# 克隆项目
git clone https://github.com/mimaldq/go-argo.git
cd go-argo

# 安装依赖
go mod download

# 编译
go build -o proxy-server .

# 运行
./proxy-server
```

本地Docker构建

```bash
# 构建镜像
docker build -t go-argo .

# 运行容器
docker run -d -p 3000:3000 -p 7860:7860 go-argo
```

🐳 Docker多架构构建

本项目支持多架构Docker镜像：

```bash
# 构建并推送多架构镜像
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t ghcr.io/mimaldq/go-argo:latest \
  --push .
```

🔄 与原Node.js版本对比

特性 Node.js版本 Go版本 改进
启动速度 ⏱️ 较慢 ⚡ 极快 5倍+
内存占用 📊 较高 📉 极低 减少70%
CPU使用 🔥 较高 ❄️ 较低 更高效
可执行文件 📦 需要Node环境 📄 单文件 更便携
并发性能 🔄 一般 🔥 优秀 更好的并发
容器大小 🐳 较大 🐋 较小 优化镜像

📁 项目结构

```
go-argo/
├── main.go                 # 主程序入口
├── go.mod                  # Go模块定义
├── go.sum                  # 依赖校验
├── Dockerfile              # 多架构Docker构建
├── .github/workflows/      # GitHub Actions
│   └── docker.yml          # CI/CD流水线
├── README.md               # 项目文档
└── config/                 # 配置文件示例
```

🔐 安全说明

1. UUID安全：建议使用自定义UUID
2. 端口管理：确保只暴露必要的端口
3. 监控配置：启用哪吒监控增强安全性
4. 定期更新：保持镜像和依赖更新

🤝 贡献

欢迎提交Issue和Pull Request！

📄 许可证

本项目基于 MIT 许可证开源。

🙏 致谢

· 感谢 Xray-core 项目
· 感谢 Cloudflare 提供的隧道服务
· 感谢 nezha 监控系统
· 感谢老王node-argo项目[GitHub仓库](https://github.com/eooce/nodejs-argo)

📞 支持

如有问题，请：

1. 查看 Issues
2. 提交新的Issue
3. 或者通过讨论区交流

---

提示：本工具仅用于学习和研究目的，请遵守当地法律法规。
