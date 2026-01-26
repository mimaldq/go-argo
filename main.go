package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v2"
)

// 环境变量配置结构
type Config struct {
	UploadURL     string
	ProjectURL    string
	AutoAccess    bool
	FilePath      string
	SubPath       string
	Port          string
	UUID          string
	NezhaServer   string
	NezhaPort     string
	NezhaKey      string
	ArgoDomain    string
	ArgoAuth      string
	ArgoPort      string
	CFIP          string
	CFPort        string
	Name          string
	MonitorKey    string
	MonitorServer string
	MonitorURL    string
}

// 全局变量
var (
	config          Config
	filePath        string
	npmName         string
	webName         string
	botName         string
	phpName         string
	monitorName     = "cf-vps-monitor.sh"
	npmPath         string
	phpPath         string
	webPath         string
	botPath         string
	monitorPath     string
	subPath         string
	listPath        string
	bootLogPath     string
	configPath      string
	tunnelJsonPath  string
	tunnelYamlPath  string
	monitorProcess  *exec.Cmd
	monitorMutex    sync.Mutex
	xrayProxy       *httputil.ReverseProxy
	httpProxy       *httputil.ReverseProxy
	argoDomain      string
	domainMutex     sync.RWMutex
)

// 隧道配置结构
type TunnelConfig struct {
	TunnelID  string `json:"TunnelID"`
	AccountID string `json:"AccountTag"`
	TunnelSecret string `json:"TunnelSecret"`
}

// 初始化配置
func initConfig() {
	config = Config{
		UploadURL:     getEnv("UPLOAD_URL", ""),
		ProjectURL:    getEnv("PROJECT_URL", ""),
		AutoAccess:    getEnvBool("AUTO_ACCESS", false),
		FilePath:      getEnv("FILE_PATH", "./tmp"),
		SubPath:       getEnv("SUB_PATH", "sub"),
		Port:          getEnv("SERVER_PORT", getEnv("PORT", "3000")),
		UUID:          getEnv("UUID", "e2cae6af-5cdd-fa48-4137-ad3e617fbab0"),
		NezhaServer:   getEnv("NEZHA_SERVER", ""),
		NezhaPort:     getEnv("NEZHA_PORT", ""),
		NezhaKey:      getEnv("NEZHA_KEY", ""),
		ArgoDomain:    getEnv("ARGO_DOMAIN", "date.goyo123.ggff.net"),
		ArgoAuth:      getEnv("ARGO_AUTH", "eyJhIjoiNWRmNTFlZjhhMTNiMWQ1ZDFhODhhZTAxNWFmYTU5OGIiLCJ0IjoiM2Q0M2I5ZTgtNDM0Zi00YjA2LTk5ZmEtMjc2ODc0MGI3ZTcyIiwicyI6Ill6SmhNemxoT1RFdFpUSTROeTAwTmpFeUxUazBOelV0WlRZNFptRTFabUV6WldKbCJ9"),
		ArgoPort:      getEnv("ARGO_PORT", "7860"),
		CFIP:          getEnv("CFIP", "cdns.doon.eu.org"),
		CFPort:        getEnv("CFPORT", "443"),
		Name:          getEnv("NAME", ""),
		MonitorKey:    getEnv("MONITOR_KEY", ""),
		MonitorServer: getEnv("MONITOR_SERVER", ""),
		MonitorURL:    getEnv("MONITOR_URL", ""),
	}

	// 创建运行文件夹
	filePath = config.FilePath
	if err := os.MkdirAll(filePath, 0755); err != nil {
		log.Printf("创建目录失败: %v", err)
	} else {
		log.Printf("目录 %s 已准备就绪", filePath)
	}

	// 生成随机文件名
	npmName = generateRandomName()
	webName = generateRandomName()
	botName = generateRandomName()
	phpName = generateRandomName()

	// 设置文件路径
	npmPath = filepath.Join(filePath, npmName)
	phpPath = filepath.Join(filePath, phpName)
	webPath = filepath.Join(filePath, webName)
	botPath = filepath.Join(filePath, botName)
	monitorPath = filepath.Join(filePath, monitorName)
	subPath = filepath.Join(filePath, "sub.txt")
	listPath = filepath.Join(filePath, "list.txt")
	bootLogPath = filepath.Join(filePath, "boot.log")
	configPath = filepath.Join(filePath, "config.json")
	tunnelJsonPath = filepath.Join(filePath, "tunnel.json")
	tunnelYamlPath = filepath.Join(filePath, "tunnel.yml")
}

// 生成随机6位字符文件名
func generateRandomName() string {
	const chars = "abcdefghijklmnopqrstuvwxyz"
	result := make([]byte, 6)
	rand.Read(result)
	for i := 0; i < 6; i++ {
		result[i] = chars[result[i]%byte(len(chars))]
	}
	return string(result)
}

// 获取环境变量
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// 获取布尔环境变量
func getEnvBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		return strings.ToLower(value) == "true"
	}
	return defaultValue
}

// HTTP代理处理函数
func proxyHandler(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path
	
	// 与Node.js完全一致的路径匹配逻辑
	if strings.HasPrefix(urlPath, "/vless-argo") || 
	   strings.HasPrefix(urlPath, "/vmess-argo") || 
	   strings.HasPrefix(urlPath, "/trojan-argo") ||
	   urlPath == "/vless" || 
	   urlPath == "/vmess" || 
	   urlPath == "/trojan" {
		// 转发到Xray端口（3001）
		if xrayProxy == nil {
			xrayURL, _ := url.Parse("http://localhost:3001")
			xrayProxy = httputil.NewSingleHostReverseProxy(xrayURL)
			xrayProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
				log.Printf("Xray代理错误: %v", err)
				if !isHeaderSent(w) {
					http.Error(w, "Xray代理错误", http.StatusInternalServerError)
				}
			}
			xrayProxy.ModifyResponse = func(resp *http.Response) error {
				// 添加安全头部
				resp.Header.Set("X-Content-Type-Options", "nosniff")
				return nil
			}
		}
		xrayProxy.ServeHTTP(w, r)
	} else {
		// 转发到HTTP服务器端口
		if httpProxy == nil {
			httpURL, _ := url.Parse("http://localhost:" + config.Port)
			httpProxy = httputil.NewSingleHostReverseProxy(httpURL)
			httpProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
				log.Printf("HTTP代理错误: %v", err)
				if !isHeaderSent(w) {
					http.Error(w, "HTTP代理错误", http.StatusInternalServerError)
				}
			}
		}
		httpProxy.ServeHTTP(w, r)
	}
}

// WebSocket代理处理器
func wsProxyHandler(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path
	
	if strings.HasPrefix(urlPath, "/vless-argo") || 
	   strings.HasPrefix(urlPath, "/vmess-argo") || 
	   strings.HasPrefix(urlPath, "/trojan-argo") {
		// 转发到Xray的WebSocket
		director := func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "localhost:3001"
			// 保持WebSocket头
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "Upgrade")
		}
		proxy := &httputil.ReverseProxy{Director: director}
		proxy.ServeHTTP(w, r)
	} else {
		// 转发到HTTP服务器
		director := func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "localhost:" + config.Port
		}
		proxy := &httputil.ReverseProxy{Director: director}
		proxy.ServeHTTP(w, r)
	}
}

// 检查是否已发送头部
func isHeaderSent(w http.ResponseWriter) bool {
	// 尝试写入空字节来检测
	return false // 简化实现
}

// 主HTTP处理函数
func mainHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" && r.URL.Path == "/" {
		// 检查index.html是否存在
		if _, err := os.Stat("index.html"); err == nil {
			http.ServeFile(w, r, "index.html")
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, "<html><body><h1>Hello world!</h1><p>Go Argo Proxy Server</p></body></html>")
		}
		return
	}
	
	if r.Method == "GET" && r.URL.Path == "/"+config.SubPath {
		// 提供订阅
		if data, err := os.ReadFile(subPath); err == nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			w.Write(data)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	
	// 其他请求处理
	http.NotFound(w, r)
}

// 删除历史节点
func deleteNodes() {
	if config.UploadURL == "" {
		log.Println("UPLOAD_URL为空，跳过删除节点")
		return
	}
	
	if _, err := os.Stat(subPath); os.IsNotExist(err) {
		log.Println("sub.txt不存在，跳过删除节点")
		return
	}
	
	content, err := os.ReadFile(subPath)
	if err != nil {
		log.Printf("读取sub.txt失败: %v", err)
		return
	}
	
	decoded, err := base64.StdEncoding.DecodeString(string(content))
	if err != nil {
		log.Printf("解码sub.txt失败: %v", err)
		return
	}
	
	lines := strings.Split(string(decoded), "\n")
	var nodes []string
	for _, line := range lines {
		if strings.Contains(line, "vless://") || strings.Contains(line, "vmess://") ||
		   strings.Contains(line, "trojan://") || strings.Contains(line, "hysteria2://") ||
		   strings.Contains(line, "tuic://") {
			nodes = append(nodes, strings.TrimSpace(line))
		}
	}
	
	if len(nodes) == 0 {
		log.Println("未找到有效节点，跳过删除")
		return
	}
	
	payload := map[string][]string{"nodes": nodes}
	jsonData, _ := json.Marshal(payload)
	
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", config.UploadURL+"/api/delete-nodes", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("创建删除请求失败: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	
	go func() {
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("删除节点请求失败: %v", err)
			return
		}
		defer resp.Body.Close()
		
		if resp.StatusCode == 200 {
			log.Println("历史节点删除成功")
		} else {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("删除节点失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
		}
	}()
}

// 清理旧文件
func cleanupOldFiles() {
	log.Println("开始清理历史文件...")
	files, err := os.ReadDir(filePath)
	if err != nil {
		log.Printf("读取目录失败: %v", err)
		return
	}
	
	cleaned := 0
	for _, file := range files {
		fileName := file.Name()
		// 保留正在使用的文件
		if fileName == "sub.txt" || fileName == "list.txt" {
			continue
		}
		
		fileFullPath := filepath.Join(filePath, fileName)
		if file.IsDir() {
			// 递归删除子目录
			os.RemoveAll(fileFullPath)
			cleaned++
		} else {
			if err := os.Remove(fileFullPath); err == nil {
				cleaned++
			}
		}
	}
	log.Printf("清理完成，删除了 %d 个文件/目录", cleaned)
}

// 生成Xray配置文件
func generateConfig() error {
	log.Println("生成Xray配置文件...")
	
	// 完整的Xray配置，与Node.js版本完全一致
	configData := map[string]interface{}{
		"log": map[string]string{
			"access":   "/dev/null",
			"error":    "/dev/null",
			"loglevel": "none",
		},
		"dns": map[string]interface{}{
			"servers": []string{
				"https+local://8.8.8.8/dns-query",
				"https+local://1.1.1.1/dns-query",
				"8.8.8.8",
				"1.1.1.1",
			},
			"queryStrategy": "UseIP",
			"disableCache":  false,
		},
		"inbounds": []map[string]interface{}{
			{
				"port": 3001,
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{
						{
							"id": config.UUID,
							"flow": "xtls-rprx-vision",
							"level": 0,
							"email": "client@example.com",
						},
					},
					"decryption": "none",
					"fallbacks": []map[string]interface{}{
						{"dest": 3002},
						{"path": "/vless-argo", "dest": 3003},
						{"path": "/vmess-argo", "dest": 3004},
						{"path": "/trojan-argo", "dest": 3005},
					},
				},
				"streamSettings": map[string]interface{}{
					"network": "tcp",
					"security": "none",
					"tcpSettings": map[string]interface{}{
						"acceptProxyProtocol": false,
					},
				},
				"sniffing": map[string]interface{}{
					"enabled": true,
					"destOverride": []string{"http", "tls", "quic"},
					"metadataOnly": false,
				},
			},
			{
				"port":     3002,
				"listen":   "127.0.0.1",
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{
						{
							"id": config.UUID,
							"level": 0,
						},
					},
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network":  "tcp",
					"security": "none",
					"tcpSettings": map[string]interface{}{
						"header": map[string]interface{}{
							"type": "none",
						},
					},
				},
			},
			{
				"port":     3003,
				"listen":   "127.0.0.1",
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{
						{
							"id": config.UUID,
							"level": 0,
						},
					},
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network":  "ws",
					"security": "none",
					"wsSettings": map[string]interface{}{
						"path": "/vless-argo",
						"headers": map[string]interface{}{
							"Host": "",
						},
					},
				},
				"sniffing": map[string]interface{}{
					"enabled":      true,
					"destOverride": []string{"http", "tls", "quic"},
					"metadataOnly": false,
				},
			},
			{
				"port":     3004,
				"listen":   "127.0.0.1",
				"protocol": "vmess",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{
						{
							"id": config.UUID,
							"alterId": 0,
							"level": 0,
							"email": "client@example.com",
						},
					},
					"disableInsecureEncryption": false,
				},
				"streamSettings": map[string]interface{}{
					"network": "ws",
					"security": "none",
					"wsSettings": map[string]interface{}{
						"path": "/vmess-argo",
						"headers": map[string]interface{}{
							"Host": "",
						},
					},
				},
				"sniffing": map[string]interface{}{
					"enabled":      true,
					"destOverride": []string{"http", "tls", "quic"},
					"metadataOnly": false,
				},
			},
			{
				"port":     3005,
				"listen":   "127.0.0.1",
				"protocol": "trojan",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{
						{
							"password": config.UUID,
							"level": 0,
							"email": "client@example.com",
						},
					},
				},
				"streamSettings": map[string]interface{}{
					"network":  "ws",
					"security": "none",
					"wsSettings": map[string]interface{}{
						"path": "/trojan-argo",
						"headers": map[string]interface{}{
							"Host": "",
						},
					},
				},
				"sniffing": map[string]interface{}{
					"enabled":      true,
					"destOverride": []string{"http", "tls", "quic"},
					"metadataOnly": false,
				},
			},
		},
		"outbounds": []map[string]interface{}{
			{
				"protocol": "freedom",
				"tag": "direct",
				"settings": map[string]interface{}{
					"domainStrategy": "UseIP",
				},
			},
			{
				"protocol": "blackhole",
				"tag": "block",
				"settings": map[string]interface{}{
					"response": map[string]interface{}{
						"type": "none",
					},
				},
			},
		},
		"routing": map[string]interface{}{
			"domainStrategy": "IPIfNonMatch",
			"rules": []map[string]interface{}{
				{
					"type": "field",
					"ip": []string{
						"geoip:private",
					},
					"outboundTag": "block",
				},
			},
		},
		"policy": map[string]interface{}{
			"levels": map[string]interface{}{
				"0": map[string]interface{}{
					"handshake": 4,
					"connIdle": 300,
					"uplinkOnly": 2,
					"downlinkOnly": 5,
					"bufferSize": 10240,
				},
			},
			"system": map[string]interface{}{
				"statsInboundUplink": true,
				"statsInboundDownlink": true,
				"statsOutboundUplink": true,
				"statsOutboundDownlink": true,
			},
		},
		"stats": map[string]interface{}{},
		"api": map[string]interface{}{
			"tag": "api",
			"services": []string{
				"StatsService",
			},
		},
	}
	
	jsonData, err := json.MarshalIndent(configData, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON序列化失败: %v", err)
	}
	
	if err := os.WriteFile(configPath, jsonData, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %v", err)
	}
	
	log.Printf("Xray配置文件生成成功: %s", configPath)
	return nil
}

// 获取系统架构
func getSystemArchitecture() string {
	arch := runtime.GOARCH
	switch arch {
	case "arm", "arm64", "aarch64":
		return "arm"
	default:
		return "amd"
	}
}

// 下载文件
func downloadFile(filePath, fileURL string) error {
	log.Printf("开始下载: %s -> %s", fileURL, filepath.Base(filePath))
	
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	
	req, err := http.NewRequest("GET", fileURL, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP错误: %s", resp.Status)
	}
	
	// 创建临时文件
	tempPath := filePath + ".tmp"
	out, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %v", err)
	}
	defer out.Close()
	
	// 使用带缓冲的拷贝
	buf := make([]byte, 32*1024)
	written, err := io.CopyBuffer(out, resp.Body, buf)
	if err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("写入文件失败: %v", err)
	}
	
	// 重命名临时文件为最终文件
	if err := os.Rename(tempPath, filePath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("重命名文件失败: %v", err)
	}
	
	// 设置执行权限
	if err := os.Chmod(filePath, 0755); err != nil {
		return fmt.Errorf("设置权限失败: %v", err)
	}
	
	log.Printf("下载完成: %s (%d bytes)", filepath.Base(filePath), written)
	return nil
}

// 根据架构获取文件URL
func getFilesForArchitecture(architecture string) []struct {
	fileName string
	fileURL  string
} {
	var files []struct {
		fileName string
		fileURL  string
	}
	
	baseURL := "https://cdn.jsdelivr.net/gh/mimaldq/go-argo@main/bin"
	
	if architecture == "arm" {
		files = []struct {
			fileName string
			fileURL  string
		}{
			{webPath, baseURL + "/arm64/xray"},
			{botPath, baseURL + "/arm64/cloudflared"},
		}
	} else {
		files = []struct {
			fileName string
			fileURL  string
		}{
			{webPath, baseURL + "/amd64/xray"},
			{botPath, baseURL + "/amd64/cloudflared"},
		}
	}
	
	if config.NezhaServer != "" && config.NezhaKey != "" {
		if config.NezhaPort != "" {
			// 哪吒v0
			if architecture == "arm" {
				files = append([]struct {
					fileName string
					fileURL  string
				}{{npmPath, baseURL + "/arm64/nezha-agent"}}, files...)
			} else {
				files = append([]struct {
					fileName string
					fileURL  string
				}{{npmPath, baseURL + "/amd64/nezha-agent"}}, files...)
			}
		} else {
			// 哪吒v1
			if architecture == "arm" {
				files = append([]struct {
					fileName string
					fileURL  string
				}{{phpPath, baseURL + "/arm64/nezha-client"}}, files...)
			} else {
				files = append([]struct {
					fileName string
					fileURL  string
				}{{phpPath, baseURL + "/amd64/nezha-client"}}, files...)
			}
		}
	}
	
	return files
}

// 下载并运行依赖文件
func downloadFilesAndRun() error {
	log.Println("开始下载和运行依赖文件...")
	
	architecture := getSystemArchitecture()
	files := getFilesForArchitecture(architecture)
	
	if len(files) == 0 {
		return fmt.Errorf("无法找到适合当前架构 %s 的文件", architecture)
	}
	
	// 并行下载文件
	var wg sync.WaitGroup
	errors := make(chan error, len(files))
	
	for _, file := range files {
		wg.Add(1)
		go func(fileName, fileURL string) {
			defer wg.Done()
			if err := downloadFile(fileName, fileURL); err != nil {
				errors <- fmt.Errorf("下载 %s 失败: %v", filepath.Base(fileName), err)
			}
		}(file.fileName, file.fileURL)
	}
	
	wg.Wait()
	close(errors)
	
	// 检查错误
	var downloadErrors []string
	for err := range errors {
		downloadErrors = append(downloadErrors, err.Error())
	}
	
	if len(downloadErrors) > 0 {
		log.Printf("部分文件下载失败: %v", downloadErrors)
		// 继续执行，不立即返回错误
	}
	
	// 运行哪吒监控
	if config.NezhaServer != "" && config.NezhaKey != "" {
		log.Println("启动哪吒监控...")
		if config.NezhaPort == "" {
			// 哪吒v1
			if err := runNezhaV1(); err != nil {
				log.Printf("哪吒v1启动失败: %v", err)
			}
		} else {
			// 哪吒v0
			if err := runNezhaV0(); err != nil {
				log.Printf("哪吒v0启动失败: %v", err)
			}
		}
		time.Sleep(2 * time.Second)
	} else {
		log.Println("哪吒监控变量为空，跳过运行")
	}
	
	// 运行Xray
	log.Println("启动Xray...")
	if err := runXray(); err != nil {
		return fmt.Errorf("Xray启动失败: %v", err)
	}
	time.Sleep(2 * time.Second)
	
	// 运行cloudflared
	log.Println("启动Cloudflare隧道...")
	if err := runCloudflared(); err != nil {
		return fmt.Errorf("Cloudflare隧道启动失败: %v", err)
	}
	
	log.Println("所有服务启动完成")
	return nil
}

// 运行哪吒v1
func runNezhaV1() error {
	// 检测哪吒是否开启TLS
	port := ""
	parts := strings.Split(config.NezhaServer, ":")
	if len(parts) > 1 {
		port = parts[1]
	}
	
	tlsPorts := map[string]bool{
		"443":  true,
		"8443": true,
		"2096": true,
		"2087": true,
		"2083": true,
		"2053": true,
	}
	
	nezhaTLS := "false"
	if tlsPorts[port] {
		nezhaTLS = "true"
	}
	
	// 生成 config.yaml
	configYaml := fmt.Sprintf(`
client_secret: %s
debug: false
disable_auto_update: true
disable_command_execute: false
disable_force_update: true
disable_nat: false
disable_send_query: false
gpu: false
insecure_tls: true
ip_report_period: 1800
report_delay: 4
server: %s
skip_connection_count: true
skip_procs_count: true
temperature: false
tls: %s
use_gitee_to_upgrade: false
use_ipv6_country_code: false
uuid: %s
`, config.NezhaKey, config.NezhaServer, nezhaTLS, config.UUID)
	
	yamlPath := filepath.Join(filePath, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(configYaml), 0644); err != nil {
		return fmt.Errorf("写入哪吒配置失败: %v", err)
	}
	
	// 运行哪吒v1
	cmd := exec.Command(phpPath, "-c", yamlPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}
	
	// 重定向输出到日志文件
	logFile, err := os.OpenFile(filepath.Join(filePath, "nezha.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		defer logFile.Close()
	}
	
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动进程失败: %v", err)
	}
	
	log.Printf("%s (哪吒v1) 运行中，PID: %d", phpName, cmd.Process.Pid)
	
	// 监控进程状态
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("哪吒v1进程退出: %v", err)
			// 可以在这里添加重启逻辑
		}
	}()
	
	return nil
}

// 运行哪吒v0
func runNezhaV0() error {
	args := []string{
		"-s", fmt.Sprintf("%s:%s", config.NezhaServer, config.NezhaPort),
		"-p", config.NezhaKey,
		"--disable-auto-update",
		"--report-delay", "4",
		"--skip-conn",
		"--skip-procs",
	}
	
	tlsPorts := []string{"443", "8443", "2096", "2087", "2083", "2053"}
	for _, tlsPort := range tlsPorts {
		if tlsPort == config.NezhaPort {
			args = append(args, "--tls")
			break
		}
	}
	
	cmd := exec.Command(npmPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}
	
	// 重定向输出到日志文件
	logFile, err := os.OpenFile(filepath.Join(filePath, "nezha.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		defer logFile.Close()
	}
	
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动进程失败: %v", err)
	}
	
	log.Printf("%s (哪吒v0) 运行中，PID: %d", npmName, cmd.Process.Pid)
	
	// 监控进程状态
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("哪吒v0进程退出: %v", err)
		}
	}()
	
	return nil
}

// 运行Xray
func runXray() error {
	if _, err := os.Stat(webPath); os.IsNotExist(err) {
		return fmt.Errorf("Xray可执行文件不存在: %s", webPath)
	}
	
	cmd := exec.Command(webPath, "-c", configPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}
	
	// 重定向输出到日志文件
	logFile, err := os.OpenFile(filepath.Join(filePath, "xray.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		defer logFile.Close()
	}
	
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动进程失败: %v", err)
	}
	
	log.Printf("%s (Xray) 运行中，PID: %d", webName, cmd.Process.Pid)
	
	// 监控进程状态
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("Xray进程退出: %v", err)
			// Xray是关键服务，尝试重启
			log.Println("尝试重启Xray...")
			time.Sleep(5 * time.Second)
			runXray()
		}
	}()
	
	return nil
}

// 运行cloudflared
func runCloudflared() error {
	if _, err := os.Stat(botPath); os.IsNotExist(err) {
		return fmt.Errorf("cloudflared可执行文件不存在: %s", botPath)
	}
	
	var args []string
	
	// 与Node.js完全一致的参数逻辑
	if config.ArgoAuth != "" && len(config.ArgoAuth) >= 120 && len(config.ArgoAuth) <= 250 && 
	   strings.Contains(config.ArgoAuth, "=") {
		// Token认证
		log.Println("使用Token连接Cloudflare隧道")
		args = []string{
			"tunnel",
			"--edge-ip-version", "auto",
			"--no-autoupdate",
			"--protocol", "http2",
			"run",
			"--token", config.ArgoAuth,
		}
	} else if config.ArgoAuth != "" && strings.Contains(config.ArgoAuth, "TunnelSecret") {
		// 隧道配置文件
		log.Println("使用TunnelSecret连接Cloudflare隧道")
		if _, err := os.Stat(tunnelYamlPath); os.IsNotExist(err) {
			return fmt.Errorf("隧道配置文件不存在: %s", tunnelYamlPath)
		}
		args = []string{
			"tunnel",
			"--edge-ip-version", "auto",
			"--no-autoupdate",
			"--protocol", "http2",
			"--config", tunnelYamlPath,
			"run",
		}
	} else {
		// 临时隧道
		log.Println("使用临时Cloudflare隧道")
		args = []string{
			"tunnel",
			"--edge-ip-version", "auto",
			"--no-autoupdate",
			"--protocol", "http2",
			"--logfile", bootLogPath,
			"--loglevel", "info",
			"--url", fmt.Sprintf("http://localhost:%s", config.ArgoPort),
		}
	}
	
	cmd := exec.Command(botPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}
	
	// 对于临时隧道，还需要捕获stdout/stderr
	if config.ArgoAuth == "" || (!strings.Contains(config.ArgoAuth, "TunnelSecret") && 
	   !(len(config.ArgoAuth) >= 120 && len(config.ArgoAuth) <= 250 && strings.Contains(config.ArgoAuth, "="))) {
		// 临时隧道需要捕获输出以提取域名
		stdoutPipe, _ := cmd.StdoutPipe()
		stderrPipe, _ := cmd.StderrPipe()
		
		go func() {
			scanner := bufio.NewScanner(stdoutPipe)
			for scanner.Scan() {
				line := scanner.Text()
				log.Printf("[cloudflared stdout] %s", line)
				// 尝试从输出中提取域名
				if domain := extractDomainFromLine(line); domain != "" {
					domainMutex.Lock()
					argoDomain = domain
					domainMutex.Unlock()
					log.Printf("从输出提取到域名: %s", domain)
				}
			}
		}()
		
		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				line := scanner.Text()
				log.Printf("[cloudflared stderr] %s", line)
			}
		}()
	}
	
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动进程失败: %v", err)
	}
	
	log.Printf("%s (cloudflared) 运行中，PID: %d", botName, cmd.Process.Pid)
	
	// 监控进程状态
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("cloudflared进程退出: %v", err)
		}
	}()
	
	return nil
}

// 从日志行中提取域名
func extractDomainFromLine(line string) string {
	// 尝试匹配多种格式的域名
	patterns := []string{
		`https?://([a-zA-Z0-9-]+\\.trycloudflare\\.com)`,
		`https?://([a-zA-Z0-9-]+\\.[a-zA-Z0-9-]+\\.cloudflaretunnel\\.com)`,
		`\\|\\s+(https?://[^\\s]+)`,
	}
	
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(line)
		if len(matches) > 1 {
			domain := matches[1]
			// 清理域名
			domain = strings.TrimPrefix(domain, "https://")
			domain = strings.TrimPrefix(domain, "http://")
			domain = strings.TrimSuffix(domain, "/")
			return domain
		}
	}
	
	return ""
}

// 处理固定隧道
func argoType() {
	log.Println("处理Cloudflare隧道配置...")
	
	if config.ArgoAuth == "" || config.ArgoDomain == "" {
		log.Println("ARGO_DOMAIN 或 ARGO_AUTH 为空，使用快速隧道")
		domainMutex.Lock()
		argoDomain = ""
		domainMutex.Unlock()
		return
	}
	
	if strings.Contains(config.ArgoAuth, "TunnelSecret") {
		log.Println("检测到TunnelSecret格式的ARGO_AUTH")
		
		// 写入隧道JSON配置
		if err := os.WriteFile(tunnelJsonPath, []byte(config.ArgoAuth), 0644); err != nil {
			log.Printf("写入隧道JSON配置失败: %v", err)
			return
		}
		
		// 解析隧道配置
		var tunnelConfig TunnelConfig
		if err := json.Unmarshal([]byte(config.ArgoAuth), &tunnelConfig); err != nil {
			log.Printf("解析隧道配置失败: %v", err)
			return
		}
		
		if tunnelConfig.TunnelID == "" {
			log.Println("隧道配置中缺少TunnelID")
			return
		}
		
		// 生成YAML配置
		tunnelYaml := TunnelYAML{
			Tunnel:       tunnelConfig.TunnelID,
			CredentialsFile: tunnelJsonPath,
			Protocol:     "http2",
			Ingress: []IngressRule{
				{
					Hostname: config.ArgoDomain,
					Service:  fmt.Sprintf("http://localhost:%s", config.ArgoPort),
					OriginRequest: &OriginRequest{
						NoTLSVerify: true,
					},
				},
				{
					Service: "http_status:404",
				},
			},
		}
		
		yamlData, err := yaml.Marshal(&tunnelYaml)
		if err != nil {
			log.Printf("生成YAML配置失败: %v", err)
			return
		}
		
		if err := os.WriteFile(tunnelYamlPath, yamlData, 0644); err != nil {
			log.Printf("写入隧道YAML配置失败: %v", err)
			return
		}
		
		log.Println("隧道YAML配置生成成功")
		domainMutex.Lock()
		argoDomain = config.ArgoDomain
		domainMutex.Unlock()
		log.Printf("使用固定域名: %s", config.ArgoDomain)
	} else {
		log.Println("ARGO_AUTH 不是TunnelSecret格式，使用token连接隧道")
		domainMutex.Lock()
		argoDomain = config.ArgoDomain
		domainMutex.Unlock()
		log.Printf("使用固定域名: %s", config.ArgoDomain)
	}
}

// YAML配置结构
type TunnelYAML struct {
	Tunnel          string        `yaml:"tunnel"`
	CredentialsFile string        `yaml:"credentials-file"`
	Protocol        string        `yaml:"protocol"`
	Ingress         []IngressRule `yaml:"ingress"`
}

type IngressRule struct {
	Hostname      string         `yaml:"hostname,omitempty"`
	Service       string         `yaml:"service"`
	OriginRequest *OriginRequest `yaml:"originRequest,omitempty"`
}

type OriginRequest struct {
	NoTLSVerify bool `yaml:"noTLSVerify"`
}

// 下载监控脚本
func downloadMonitorScript() bool {
	if config.MonitorKey == "" || config.MonitorServer == "" || config.MonitorURL == "" {
		log.Println("监控环境变量不完整，跳过监控脚本启动")
		return false
	}
	
	monitorURL := "https://raw.githubusercontent.com/mimaldq/cf-vps-monitor/main/cf-vps-monitor.sh"
	log.Printf("从 %s 下载监控脚本", monitorURL)
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(monitorURL)
	if err != nil {
		log.Printf("下载监控脚本失败: %v", err)
		return false
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		log.Printf("下载监控脚本失败，状态码: %d", resp.StatusCode)
		return false
	}
	
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("读取监控脚本失败: %v", err)
		return false
	}
	
	if err := os.WriteFile(monitorPath, data, 0755); err != nil {
		log.Printf("保存监控脚本失败: %v", err)
		return false
	}
	
	log.Println("监控脚本下载完成")
	return true
}

// 运行监控脚本
func runMonitorScript() {
	if config.MonitorKey == "" || config.MonitorServer == "" || config.MonitorURL == "" {
		return
	}
	
	monitorMutex.Lock()
	defer monitorMutex.Unlock()
	
	// 如果已有监控进程在运行，先停止
	if monitorProcess != nil && monitorProcess.Process != nil {
		log.Println("停止现有的监控脚本进程")
		monitorProcess.Process.Kill()
		monitorProcess.Wait()
	}
	
	args := []string{
		"-i",
		"-k", config.MonitorKey,
		"-s", config.MonitorServer,
		"-u", config.MonitorURL,
	}
	
	log.Printf("运行监控脚本: %s %s", monitorPath, strings.Join(args, " "))
	
	cmd := exec.Command(monitorPath, args...)
	monitorProcess = cmd
	
	// 捕获输出
	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()
	
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			log.Printf("[监控脚本] %s", scanner.Text())
		}
	}()
	
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			log.Printf("[监控脚本错误] %s", scanner.Text())
		}
	}()
	
	if err := cmd.Start(); err != nil {
		log.Printf("启动监控脚本失败: %v", err)
		return
	}
	
	log.Printf("监控脚本运行中，PID: %d", cmd.Process.Pid)
	
	// 监控进程状态
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("监控脚本退出，错误: %v", err)
			log.Println("将在30秒后重启监控脚本...")
			time.Sleep(30 * time.Second)
			runMonitorScript()
		}
	}()
}

// 启动监控脚本
func startMonitorScript() {
	if config.MonitorKey == "" || config.MonitorServer == "" || config.MonitorURL == "" {
		log.Println("监控脚本未配置，跳过")
		return
	}
	
	log.Println("等待其他服务启动...")
	time.Sleep(10 * time.Second)
	
	if downloaded := downloadMonitorScript(); downloaded {
		runMonitorScript()
	}
}

// 提取临时隧道域名
func extractDomains() error {
	log.Println("提取隧道域名...")
	
	domainMutex.RLock()
	currentDomain := argoDomain
	domainMutex.RUnlock()
	
	if currentDomain != "" {
		log.Printf("已有域名: %s，跳过提取", currentDomain)
		return generateLinks(currentDomain)
	}
	
	// 检查日志文件获取临时域名
	if _, err := os.Stat(bootLogPath); os.IsNotExist(err) {
		log.Printf("日志文件不存在: %s", bootLogPath)
		return fmt.Errorf("日志文件不存在")
	}
	
	// 等待日志文件生成
	for i := 0; i < 10; i++ {
		data, err := os.ReadFile(bootLogPath)
		if err != nil {
			log.Printf("读取日志文件失败: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if domain := extractDomainFromLine(line); domain != "" {
				domainMutex.Lock()
				argoDomain = domain
				domainMutex.Unlock()
				log.Printf("从日志提取到域名: %s", domain)
				return generateLinks(domain)
			}
		}
		
		log.Printf("第 %d 次尝试，未找到域名，等待中...", i+1)
		time.Sleep(3 * time.Second)
	}
	
	log.Println("未找到域名，重新运行cloudflared以获取Argo域名")
	
	// 停止现有的cloudflared进程
	killBotProcess()
	time.Sleep(3 * time.Second)
	
	// 重新启动cloudflared
	args := []string{
		"tunnel",
		"--edge-ip-version", "auto",
		"--no-autoupdate",
		"--protocol", "http2",
		"--logfile", bootLogPath,
		"--loglevel", "info",
		"--url", fmt.Sprintf("http://localhost:%s", config.ArgoPort),
	}
	
	cmd := exec.Command(botPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	
	// 捕获输出以提取域名
	stdoutPipe, _ := cmd.StdoutPipe()
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("[cloudflared重启] %s", line)
			if domain := extractDomainFromLine(line); domain != "" {
				domainMutex.Lock()
				argoDomain = domain
				domainMutex.Unlock()
				log.Printf("从重启输出提取到域名: %s", domain)
			}
		}
	}()
	
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("重启cloudflared失败: %v", err)
	}
	
	log.Printf("%s 重新运行中", botName)
	time.Sleep(5 * time.Second)
	
	// 再次尝试提取域名
	return extractDomains()
}

// 杀死bot进程
func killBotProcess() {
	log.Println("停止cloudflared进程...")
	
	if runtime.GOOS == "windows" {
		exec.Command("taskkill", "/f", "/im", botName+".exe").Run()
	} else {
		// 尝试多种方式杀死进程
		exec.Command("pkill", "-f", botName).Run()
		exec.Command("pkill", "-f", "cloudflared").Run()
		exec.Command("killall", botName).Run()
		exec.Command("killall", "cloudflared").Run()
	}
	
	time.Sleep(1 * time.Second)
}

// 获取ISP信息
func getMetaInfo() string {
	log.Println("获取ISP信息...")
	
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout: 3 * time.Second,
			}).Dial,
		},
	}
	
	// 尝试第一个API
	resp, err := client.Get("https://ipapi.co/json/")
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var data map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
				if countryCode, ok := data["country_code"].(string); ok {
					if org, ok := data["org"].(string); ok {
						// 清理组织名称
						org = strings.Split(org, ",")[0]
						org = strings.ReplaceAll(org, " ", "_")
						result := fmt.Sprintf("%s_%s", countryCode, org)
						log.Printf("从ipapi.co获取到ISP: %s", result)
						return result
					}
				}
			}
		}
	} else {
		log.Printf("ipapi.co请求失败: %v", err)
	}
	
	// 尝试第二个API
	resp, err = client.Get("http://ip-api.com/json/")
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var data map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
				if status, ok := data["status"].(string); ok && status == "success" {
					if countryCode, ok := data["countryCode"].(string); ok {
						if org, ok := data["org"].(string); ok {
							// 清理组织名称
							org = strings.Split(org, ",")[0]
							org = strings.ReplaceAll(org, " ", "_")
							result := fmt.Sprintf("%s_%s", countryCode, org)
							log.Printf("从ip-api.com获取到ISP: %s", result)
							return result
						}
					}
				}
			}
		}
	} else {
		log.Printf("ip-api.com请求失败: %v", err)
	}
	
	log.Println("无法获取ISP信息，使用Unknown")
	return "Unknown"
}

// 生成订阅链接
func generateLinks(argoDomain string) error {
	log.Println("生成订阅链接...")
	
	isp := getMetaInfo()
	nodeName := isp
	if config.Name != "" {
		nodeName = fmt.Sprintf("%s-%s", config.Name, isp)
	}
	
	// URL编码路径，与Node.js完全一致
	encodedPath := "%2Fvless-argo%3Fed%3D2560"
	
	// 生成VMESS配置
	vmessConfig := map[string]interface{}{
		"v":    "2",
		"ps":   nodeName,
		"add":  config.CFIP,
		"port": config.CFPort,
		"id":   config.UUID,
		"aid":  "0",
		"scy":  "none",
		"net":  "ws",
		"type": "none",
		"host": argoDomain,
		"path": "/vmess-argo?ed=2560",
		"tls":  "tls",
		"sni":  argoDomain,
		"alpn": "",
		"fp":   "firefox",
	}
	
	vmessJSON, err := json.Marshal(vmessConfig)
	if err != nil {
		return fmt.Errorf("序列化VMESS配置失败: %v", err)
	}
	
	vmessBase64 := base64.StdEncoding.EncodeToString(vmessJSON)
	
	// 生成订阅文本，与Node.js完全一致的格式
	subTxt := fmt.Sprintf(`vless://%s@%s:%s?encryption=none&security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%s#%s

vmess://%s

trojan://%s@%s:%s?security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%s#%s`,
		config.UUID, config.CFIP, config.CFPort, argoDomain, argoDomain, encodedPath, nodeName,
		vmessBase64,
		config.UUID, config.CFIP, config.CFPort, argoDomain, argoDomain, encodedPath, nodeName)
	
	// 打印到控制台（base64编码）
	encoded := base64.StdEncoding.EncodeToString([]byte(subTxt))
	log.Println("订阅内容base64编码:")
	log.Println(encoded)
	
	// 保存文件
	if err := os.WriteFile(subPath, []byte(encoded), 0644); err != nil {
		return fmt.Errorf("保存订阅文件失败: %v", err)
	}
	
	log.Printf("%s/sub.txt 保存成功", filePath)
	
	// 上传节点
	go uploadNodes()
	
	return nil
}

// 上传节点或订阅
func uploadNodes() {
	if config.UploadURL == "" {
		log.Println("UPLOAD_URL为空，跳过上传")
		return
	}
	
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		},
	}
	
	if config.ProjectURL != "" {
		// 上传订阅
		log.Println("开始上传订阅...")
		subscriptionUrl := fmt.Sprintf("%s/%s", config.ProjectURL, config.SubPath)
		payload := map[string][]string{
			"subscription": {subscriptionUrl},
		}
		
		jsonData, _ := json.Marshal(payload)
		
		req, err := http.NewRequest("POST", config.UploadURL+"/api/add-subscriptions", bytes.NewBuffer(jsonData))
		if err != nil {
			log.Printf("创建订阅上传请求失败: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("订阅上传请求失败: %v", err)
			return
		}
		defer resp.Body.Close()
		
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == 200 {
			log.Println("订阅上传成功")
		} else if resp.StatusCode == 400 {
			log.Println("订阅已存在")
		} else {
			log.Printf("订阅上传失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
		}
	} else {
		// 上传节点
		log.Println("开始上传节点...")
		if _, err := os.Stat(listPath); os.IsNotExist(err) {
			log.Println("list.txt不存在，跳过上传节点")
			return
		}
		
		data, err := os.ReadFile(listPath)
		if err != nil {
			log.Printf("读取list.txt失败: %v", err)
			return
		}
		
		lines := strings.Split(string(data), "\n")
		var nodes []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "vless://") || strings.HasPrefix(line, "vmess://") ||
			   strings.HasPrefix(line, "trojan://") || strings.HasPrefix(line, "hysteria2://") ||
			   strings.HasPrefix(line, "tuic://") {
				nodes = append(nodes, line)
			}
			// 限制上传数量
			if len(nodes) >= 100 {
				break
			}
		}
		
		if len(nodes) == 0 {
			log.Println("未找到有效节点，跳过上传")
			return
		}
		
		payload := map[string][]string{"nodes": nodes}
		jsonData, _ := json.Marshal(payload)
		
		req, err := http.NewRequest("POST", config.UploadURL+"/api/add-nodes", bytes.NewBuffer(jsonData))
		if err != nil {
			log.Printf("创建节点上传请求失败: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("节点上传请求失败: %v", err)
			return
		}
		defer resp.Body.Close()
		
		if resp.StatusCode == 200 {
			log.Println("节点上传成功")
		} else {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("节点上传失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
		}
	}
}

// 清理文件
func cleanFiles() {
	log.Println("设置90秒后清理临时文件...")
	
	time.AfterFunc(90*time.Second, func() {
		log.Println("开始清理临时文件...")
		
		filesToDelete := []string{
			bootLogPath, configPath, webPath, botPath, monitorPath,
			filepath.Join(filePath, "config.yaml"),
			filepath.Join(filePath, "nezha.log"),
			filepath.Join(filePath, "xray.log"),
		}
		
		if config.NezhaPort != "" {
			filesToDelete = append(filesToDelete, npmPath)
		} else if config.NezhaServer != "" && config.NezhaKey != "" {
			filesToDelete = append(filesToDelete, phpPath)
		}
		
		deletedCount := 0
		for _, file := range filesToDelete {
			if _, err := os.Stat(file); err == nil {
				if err := os.Remove(file); err == nil {
					deletedCount++
				} else {
					log.Printf("删除文件失败 %s: %v", file, err)
				}
			}
		}
		
		log.Printf("清理完成，删除了 %d 个临时文件", deletedCount)
		log.Println("应用正在运行")
		log.Println("感谢使用此脚本，享受吧！")
	})
}

// 自动访问项目URL
func addVisitTask() {
	if !config.AutoAccess || config.ProjectURL == "" {
		log.Println("跳过添加自动访问任务")
		return
	}
	
	log.Println("添加自动访问任务...")
	
	payload := map[string]string{"url": config.ProjectURL}
	jsonData, _ := json.Marshal(payload)
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post("https://oooo.serv00.net/add-url", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("添加自动访问任务失败: %v", err)
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 200 {
		log.Println("自动访问任务添加成功")
	} else {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("添加自动访问任务失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}
}

// 主运行逻辑
func startServer() {
	log.Println("开始服务器初始化...")
	
	deleteNodes()
	cleanupOldFiles()
	
	argoType()
	
	if err := generateConfig(); err != nil {
		log.Printf("生成配置文件失败: %v", err)
		return
	}
	
	if err := downloadFilesAndRun(); err != nil {
		log.Printf("下载并运行文件失败: %v", err)
		return
	}
	
	log.Println("等待隧道启动...")
	time.Sleep(5 * time.Second)
	
	if err := extractDomains(); err != nil {
		log.Printf("提取域名失败: %v", err)
	}
	
	addVisitTask()
	log.Println("服务器初始化完成")
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Go Argo Proxy Server 启动中...")
	
	// 初始化配置
	initConfig()
	
	// 创建内部HTTP服务器
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/", mainHandler)
	
	internalServer := &http.Server{
		Addr:    ":" + config.Port,
		Handler: httpMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}
	
	// 创建代理服务器
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			wsProxyHandler(w, r)
		} else {
			proxyHandler(w, r)
		}
	})
	
	proxyServer := &http.Server{
		Addr:    ":" + config.ArgoPort,
		Handler: proxyMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}
	
	// 启动内部HTTP服务器
	go func() {
		log.Printf("HTTP服务运行在内部端口: %s", config.Port)
		if err := internalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP服务器错误: %v", err)
		}
	}()
	
	// 启动代理服务器
	go func() {
		log.Printf("代理服务器启动在端口: %s", config.ArgoPort)
		log.Printf("HTTP流量 -> localhost:%s", config.Port)
		log.Printf("Xray流量 -> localhost:3001")
		
		if err := proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("代理服务器错误: %v", err)
		}
	}()
	
	// 启动主流程
	go startServer()
	
	// 启动监控脚本
	go startMonitorScript()
	
	// 清理文件
	cleanFiles()
	
	// 等待终止信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	
	log.Println("服务器已启动，按 Ctrl+C 停止")
	
	<-sigChan
	log.Println("收到关闭信号，正在清理...")
	
	// 停止监控脚本
	if monitorProcess != nil {
		log.Println("停止监控脚本...")
		monitorProcess.Process.Kill()
	}
	
	// 停止cloudflared进程
	killBotProcess()
	
	// 优雅关闭服务器
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := internalServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP服务器关闭错误: %v", err)
	}
	
	if err := proxyServer.Shutdown(ctx); err != nil {
		log.Printf("代理服务器关闭错误: %v", err)
	}
	
	log.Println("程序退出")
}
