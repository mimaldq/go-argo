package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// 环境变量配置
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

var (
	cfg          Config
	monitorCmd   *exec.Cmd
	proxy        *httputil.ReverseProxy
	fileNames    = make(map[string]string)
	mu           sync.RWMutex
)

func init() {
	// 从环境变量读取配置
	cfg.UploadURL = getEnv("UPLOAD_URL", "")
	cfg.ProjectURL = getEnv("PROJECT_URL", "")
	cfg.AutoAccess = getEnv("AUTO_ACCESS", "false") == "true"
	cfg.FilePath = getEnv("FILE_PATH", "./tmp")
	cfg.SubPath = getEnv("SUB_PATH", "sub")
	cfg.Port = getEnv("SERVER_PORT", getEnv("PORT", "3000"))
	cfg.UUID = getEnv("UUID", "e2cae6af-5cdd-fa48-4137-ad3e617fbab0")
	cfg.NezhaServer = getEnv("NEZHA_SERVER", "")
	cfg.NezhaPort = getEnv("NEZHA_PORT", "")
	cfg.NezhaKey = getEnv("NEZHA_KEY", "")
	cfg.ArgoDomain = getEnv("ARGO_DOMAIN", "date.goyo123.ggff.net")
	cfg.ArgoAuth = getEnv("ARGO_AUTH", "eyJhIjoiNWRmNTFlZjhhMTNiMWQ1ZDFhODhhZTAxNWFmYTU5OGIiLCJ0IjoiM2Q0M2I5ZTgtNDM0Zi00YjA2LTk5ZmEtMjc2ODc0MGI3ZTcyIiwicyI6Ill6SmhNemxoT1RFdFpUSTROeTAwTmpFeUxUazBOelV0WlRZNFptRTFabUV6WldKbCJ9")
	cfg.ArgoPort = getEnv("ARGO_PORT", "7860")
	cfg.CFIP = getEnv("CFIP", "cdns.doon.eu.org")
	cfg.CFPort = getEnv("CFPORT", "443")
	cfg.Name = getEnv("NAME", "")
	cfg.MonitorKey = getEnv("MONITOR_KEY", "")
	cfg.MonitorServer = getEnv("MONITOR_SERVER", "")
	cfg.MonitorURL = getEnv("MONITOR_URL", "")

	// 生成随机文件名
	fileNames["npm"] = generateRandomName()
	fileNames["web"] = generateRandomName()
	fileNames["bot"] = generateRandomName()
	fileNames["php"] = generateRandomName()
	fileNames["monitor"] = "cf-vps-monitor.sh"

	// 创建运行目录
	if err := os.MkdirAll(cfg.FilePath, 0755); err != nil {
		log.Fatalf("创建目录失败: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func generateRandomName() string {
	const charset = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 6)
	rand.Read(b)
	for i := range b {
		b[i] = charset[b[i]%byte(len(charset))]
	}
	return string(b)
}

func main() {
	// 设置代理服务器
	setupProxyServer()

	// 设置HTTP服务器
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/"+cfg.SubPath, handleSub)

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: mux,
	}

	// 启动HTTP服务器
	go func() {
		log.Printf("HTTP服务运行在内部端口: %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP服务器错误: %v", err)
		}
	}()

	// 启动主流程
	go startServer()

	// 启动监控脚本
	go startMonitorScript()

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 等待退出信号
	<-sigChan
	log.Println("收到关闭信号，正在清理...")

	// 停止监控脚本
	if monitorCmd != nil && monitorCmd.Process != nil {
		monitorCmd.Process.Kill()
	}

	// 清理文件
	cleanFiles()

	// 关闭HTTP服务器
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)

	log.Println("程序退出")
}

func setupProxyServer() {
	// 创建反向代理
	proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			targetURL := ""
			path := req.URL.Path

			if strings.HasPrefix(path, "/vless-argo") ||
				strings.HasPrefix(path, "/vmess-argo") ||
				strings.HasPrefix(path, "/trojan-argo") ||
				path == "/vless" ||
				path == "/vmess" ||
				path == "/trojan" {
				// 转发到Xray端口
				targetURL = "http://localhost:3001"
			} else {
				// 转发到HTTP服务器端口
				targetURL = "http://localhost:" + cfg.Port
			}

			target, _ := url.Parse(targetURL)
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = singleJoiningSlash(target.Path, req.URL.Path)
			req.Host = target.Host
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("代理错误: %v", err)
			http.Error(w, "代理错误", http.StatusInternalServerError)
		},
	}

	// WebSocket升级器
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	// 代理服务器处理函数
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查是否为WebSocket升级请求
		if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
			strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			// WebSocket连接
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				log.Printf("WebSocket升级失败: %v", err)
				return
			}
			defer conn.Close()

			// 这里可以添加WebSocket代理逻辑
			// 由于复杂性，需要额外的WebSocket代理实现
		} else {
			// HTTP请求
			proxy.ServeHTTP(w, r)
		}
	})

	// 启动代理服务器
	go func() {
		log.Printf("代理服务器启动在端口: %s", cfg.ArgoPort)
		log.Printf("HTTP流量 -> localhost:%s", cfg.Port)
		log.Printf("Xray流量 -> localhost:3001")
		if err := http.ListenAndServe(":"+cfg.ArgoPort, handler); err != nil {
			log.Fatalf("代理服务器错误: %v", err)
		}
	}()
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	indexPath := "index.html"
	if _, err := os.Stat(indexPath); err == nil {
		http.ServeFile(w, r, indexPath)
	} else {
		w.Write([]byte("Hello world!"))
	}
}

func handleSub(w http.ResponseWriter, r *http.Request) {
	// 这里应该返回订阅内容
	// 由于需要动态生成，这里返回占位符
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("Subscription content will be generated"))
}

func deleteNodes() {
	if cfg.UploadURL == "" {
		return
	}

	subPath := filepath.Join(cfg.FilePath, "sub.txt")
	if _, err := os.Stat(subPath); os.IsNotExist(err) {
		return
	}

	content, err := os.ReadFile(subPath)
	if err != nil {
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(string(content))
	if err != nil {
		return
	}

	lines := strings.Split(string(decoded), "\n")
	var nodes []string
	nodeRegex := regexp.MustCompile(`(vless|vmess|trojan|hysteria2|tuic)://`)

	for _, line := range lines {
		if nodeRegex.MatchString(line) {
			nodes = append(nodes, line)
		}
	}

	if len(nodes) == 0 {
		return
	}

	data := map[string][]string{"nodes": nodes}
	jsonData, _ := json.Marshal(data)

	_, err = http.Post(cfg.UploadURL+"/api/delete-nodes", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		// 忽略错误
	}
}

func cleanupOldFiles() {
	files, err := os.ReadDir(cfg.FilePath)
	if err != nil {
		return
	}

	for _, file := range files {
		filePath := filepath.Join(cfg.FilePath, file.Name())
		os.Remove(filePath)
	}
}

func generateConfig() error {
	config := map[string]interface{}{
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
				"port":     3001,
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]string{
						{
							"id":   cfg.UUID,
							"flow": "xtls-rprx-vision",
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
				},
			},
			{
				"port":     3002,
				"listen":   "127.0.0.1",
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]string{{"id": cfg.UUID}},
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network":  "tcp",
					"security": "none",
				},
			},
			{
				"port":     3003,
				"listen":   "127.0.0.1",
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]string{{"id": cfg.UUID, "level": "0"}},
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network":  "ws",
					"security": "none",
					"wsSettings": map[string]string{
						"path": "/vless-argo",
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
						{"id": cfg.UUID, "alterId": 0},
					},
				},
				"streamSettings": map[string]interface{}{
					"network": "ws",
					"wsSettings": map[string]string{
						"path": "/vmess-argo",
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
					"clients": []map[string]string{{"password": cfg.UUID}},
				},
				"streamSettings": map[string]interface{}{
					"network":  "ws",
					"security": "none",
					"wsSettings": map[string]string{
						"path": "/trojan-argo",
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
				"tag":      "direct",
				"settings": map[string]interface{}{
					"domainStrategy": "UseIP",
				},
			},
			{
				"protocol": "blackhole",
				"tag":      "block",
				"settings": map[string]interface{}{},
			},
		},
		"routing": map[string]interface{}{
			"domainStrategy": "IPIfNonMatch",
			"rules":          []interface{}{},
		},
	}

	configPath := filepath.Join(cfg.FilePath, "config.json")
	jsonData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(configPath, jsonData, 0644); err != nil {
		return err
	}

	log.Println("Xray配置文件生成完成")
	return nil
}

func getSystemArchitecture() string {
	arch := runtime.GOARCH
	if arch == "arm" || arch == "arm64" {
		return "arm"
	}
	return "amd"
}

func downloadFile(filePath, fileURL string) error {
	resp, err := http.Get(fileURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	if err := os.Chmod(filePath, 0755); err != nil {
		return err
	}

	log.Printf("下载 %s 成功", filepath.Base(filePath))
	return nil
}

func getFilesForArchitecture(arch string) map[string]string {
	files := make(map[string]string)
	baseURL := ""

	if arch == "arm" {
		baseURL = "https://arm64.ssss.nyc.mn/"
	} else {
		baseURL = "https://amd64.ssss.nyc.mn/"
	}

	webPath := filepath.Join(cfg.FilePath, fileNames["web"])
	botPath := filepath.Join(cfg.FilePath, fileNames["bot"])

	files[webPath] = baseURL + "web"
	files[botPath] = baseURL + "bot"

	if cfg.NezhaServer != "" && cfg.NezhaKey != "" {
		if cfg.NezhaPort != "" {
			npmPath := filepath.Join(cfg.FilePath, fileNames["npm"])
			files[npmPath] = baseURL + "agent"
		} else {
			phpPath := filepath.Join(cfg.FilePath, fileNames["php"])
			files[phpPath] = baseURL + "v1"
		}
	}

	return files
}

func downloadFilesAndRun() error {
	arch := getSystemArchitecture()
	files := getFilesForArchitecture(arch)

	if len(files) == 0 {
		return fmt.Errorf("无法找到适合当前架构的文件")
	}

	// 下载文件
	for filePath, fileURL := range files {
		if err := downloadFile(filePath, fileURL); err != nil {
			log.Printf("下载文件失败: %v", err)
			continue
		}
	}

	// 运行哪吒监控
	if cfg.NezhaServer != "" && cfg.NezhaKey != "" {
		if cfg.NezhaPort == "" {
			// 哪吒v1
			runNezhaV1()
		} else {
			// 哪吒v0
			runNezhaV0()
		}
	} else {
		log.Println("哪吒监控变量为空，跳过运行")
	}

	// 运行Xray
	webPath := filepath.Join(cfg.FilePath, fileNames["web"])
	configPath := filepath.Join(cfg.FilePath, "config.json")
	go runCommand(webPath, "-c", configPath)
	log.Printf("%s 运行中", fileNames["web"])

	time.Sleep(1 * time.Second)

	// 运行cloudflared
	runCloudflared()

	time.Sleep(2 * time.Second)
	return nil
}

func runNezhaV1() {
	// 检测哪吒是否开启TLS
	port := ""
	if strings.Contains(cfg.NezhaServer, ":") {
		parts := strings.Split(cfg.NezhaServer, ":")
		if len(parts) > 1 {
			port = parts[1]
		}
	}

	tlsPorts := map[string]bool{
		"443":  true,
		"8443": true,
		"2096": true,
		"2087": true,
		"2083": true,
		"2053": true,
	}

	nezhatls := "false"
	if tlsPorts[port] {
		nezhatls = "true"
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
uuid: %s`, cfg.NezhaKey, cfg.NezhaServer, nezhatls, cfg.UUID)

	configPath := filepath.Join(cfg.FilePath, "config.yaml")
	os.WriteFile(configPath, []byte(configYaml), 0644)

	phpPath := filepath.Join(cfg.FilePath, fileNames["php"])
	go runCommand(phpPath, "-c", configPath)
	log.Printf("%s 运行中", fileNames["php"])
}

func runNezhaV0() {
	args := []string{
		"-s", cfg.NezhaServer + ":" + cfg.NezhaPort,
		"-p", cfg.NezhaKey,
	}

	tlsPorts := map[string]bool{
		"443":  true,
		"8443": true,
		"2096": true,
		"2087": true,
		"2083": true,
		"2053": true,
	}

	if tlsPorts[cfg.NezhaPort] {
		args = append(args, "--tls")
	}

	args = append(args, "--disable-auto-update", "--report-delay", "4", "--skip-conn", "--skip-procs")

	npmPath := filepath.Join(cfg.FilePath, fileNames["npm"])
	go runCommand(npmPath, args...)
	log.Printf("%s 运行中", fileNames["npm"])
}

func runCloudflared() {
	botPath := filepath.Join(cfg.FilePath, fileNames["bot"])
	if _, err := os.Stat(botPath); os.IsNotExist(err) {
		log.Println("cloudflared 文件不存在")
		return
	}

	args := []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate", "--protocol", "http2"}

	if cfg.ArgoAuth != "" && len(cfg.ArgoAuth) >= 120 && len(cfg.ArgoAuth) <= 250 {
		// Token格式
		args = append(args, "run", "--token", cfg.ArgoAuth)
	} else if cfg.ArgoAuth != "" && strings.Contains(cfg.ArgoAuth, "TunnelSecret") {
		// TunnelSecret格式
		tunnelYamlPath := filepath.Join(cfg.FilePath, "tunnel.yml")
		if _, err := os.Stat(tunnelYamlPath); os.IsNotExist(err) {
			log.Println("等待隧道配置文件生成...")
			time.Sleep(1 * time.Second)
		}
		args = append(args, "--config", tunnelYamlPath, "run")
	} else {
		bootLogPath := filepath.Join(cfg.FilePath, "boot.log")
		args = append(args, "--logfile", bootLogPath, "--loglevel", "info",
			"--url", "http://localhost:"+cfg.ArgoPort)
	}

	go runCommand(botPath, args...)
	log.Printf("%s 运行中", fileNames["bot"])

	log.Println("等待隧道启动...")
	time.Sleep(5 * time.Second)
}

func runCommand(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Start()
}

func argoType() {
	if cfg.ArgoAuth == "" || cfg.ArgoDomain == "" {
		log.Println("ARGO_DOMAIN 或 ARGO_AUTH 为空，使用快速隧道")
		return
	}

	if strings.Contains(cfg.ArgoAuth, "TunnelSecret") {
		tunnelJsonPath := filepath.Join(cfg.FilePath, "tunnel.json")
		if err := os.WriteFile(tunnelJsonPath, []byte(cfg.ArgoAuth), 0644); err != nil {
			log.Printf("写入隧道配置文件错误: %v", err)
			return
		}

		var tunnelConfig map[string]interface{}
		if err := json.Unmarshal([]byte(cfg.ArgoAuth), &tunnelConfig); err != nil {
			log.Printf("解析隧道配置错误: %v", err)
			return
		}

		tunnelID, ok := tunnelConfig["TunnelID"].(string)
		if !ok {
			log.Println("无法获取TunnelID")
			return
		}

		tunnelYaml := fmt.Sprintf(`
tunnel: %s
credentials-file: %s
protocol: http2

ingress:
  - hostname: %s
    service: http://localhost:%s
    originRequest:
      noTLSVerify: true
  - service: http_status:404`, tunnelID, tunnelJsonPath, cfg.ArgoDomain, cfg.ArgoPort)

		tunnelYamlPath := filepath.Join(cfg.FilePath, "tunnel.yml")
		if err := os.WriteFile(tunnelYamlPath, []byte(tunnelYaml), 0644); err != nil {
			log.Printf("写入隧道YAML配置错误: %v", err)
			return
		}

		log.Println("隧道YAML配置生成成功")
	} else {
		log.Println("ARGO_AUTH 不是TunnelSecret格式，使用token连接隧道")
	}
}

func getMetaInfo() string {
	client := &http.Client{Timeout: 3 * time.Second}

	// 尝试第一个API
	resp, err := client.Get("https://ipapi.co/json/")
	if err == nil {
		defer resp.Body.Close()
		var data map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&data) == nil {
			countryCode, _ := data["country_code"].(string)
			org, _ := data["org"].(string)
			if countryCode != "" && org != "" {
				return countryCode + "_" + org
			}
		}
	}

	// 尝试备用API
	resp, err = client.Get("http://ip-api.com/json/")
	if err == nil {
		defer resp.Body.Close()
		var data map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&data) == nil {
			if status, _ := data["status"].(string); status == "success" {
				countryCode, _ := data["countryCode"].(string)
				org, _ := data["org"].(string)
				if countryCode != "" && org != "" {
					return countryCode + "_" + org
				}
			}
		}
	}

	return "Unknown"
}

func extractDomains() (string, error) {
	if cfg.ArgoAuth != "" && cfg.ArgoDomain != "" {
		log.Printf("使用固定域名: %s", cfg.ArgoDomain)
		generateLinks(cfg.ArgoDomain)
		return cfg.ArgoDomain, nil
	}

	bootLogPath := filepath.Join(cfg.FilePath, "boot.log")
	content, err := os.ReadFile(bootLogPath)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(content), "\n")
	re := regexp.MustCompile(`https?://([^ ]*trycloudflare\.com)/?`)

	for _, line := range lines {
		matches := re.FindStringSubmatch(line)
		if len(matches) > 1 {
			domain := matches[1]
			log.Printf("找到临时域名: %s", domain)
			generateLinks(domain)
			return domain, nil
		}
	}

	log.Println("未找到域名，重新运行bot以获取Argo域名")
	os.Remove(bootLogPath)

	// 重新启动cloudflared
	botPath := filepath.Join(cfg.FilePath, fileNames["bot"])
	args := []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate",
		"--protocol", "http2", "--logfile", bootLogPath,
		"--loglevel", "info", "--url", "http://localhost:" + cfg.ArgoPort}

	go runCommand(botPath, args...)
	time.Sleep(3 * time.Second)

	return extractDomains()
}

func generateLinks(argoDomain string) {
	ISP := getMetaInfo()
	nodeName := cfg.Name
	if nodeName != "" {
		nodeName = nodeName + "-" + ISP
	} else {
		nodeName = ISP
	}

	time.Sleep(2 * time.Second)

	// 生成VMESS配置
	vmessConfig := map[string]interface{}{
		"v":    "2",
		"ps":   nodeName,
		"add":  cfg.CFIP,
		"port": cfg.CFPort,
		"id":   cfg.UUID,
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

	vmessJSON, _ := json.Marshal(vmessConfig)
	vmessBase64 := base64.StdEncoding.EncodeToString(vmessJSON)

	subTxt := fmt.Sprintf(`
vless://%s@%s:%s?encryption=none&security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Fvless-argo%%3Fed%%3D2560#%s

vmess://%s

trojan://%s@%s:%s?security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Ftrojan-argo%%3Fed%%3D2560#%s
`, cfg.UUID, cfg.CFIP, cfg.CFPort, argoDomain, argoDomain, nodeName,
		vmessBase64, cfg.UUID, cfg.CFIP, cfg.CFPort, argoDomain, argoDomain, nodeName)

	// 保存到文件
	subPath := filepath.Join(cfg.FilePath, "sub.txt")
	encodedContent := base64.StdEncoding.EncodeToString([]byte(subTxt))
	os.WriteFile(subPath, []byte(encodedContent), 0644)

	log.Printf("%s/sub.txt 保存成功", cfg.FilePath)
	log.Println("Base64编码内容:", encodedContent)

	uploadNodes()
}

func uploadNodes() {
	if cfg.UploadURL == "" {
		return
	}

	if cfg.ProjectURL != "" {
		// 上传订阅
		subscriptionUrl := cfg.ProjectURL + "/" + cfg.SubPath
		data := map[string][]string{"subscription": {subscriptionUrl}}
		jsonData, _ := json.Marshal(data)

		resp, err := http.Post(cfg.UploadURL+"/api/add-subscriptions", "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			log.Printf("订阅上传失败: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			log.Println("订阅上传成功")
		} else if resp.StatusCode == 400 {
			log.Println("订阅已存在")
		}
	} else {
		// 上传节点
		listPath := filepath.Join(cfg.FilePath, "list.txt")
		if _, err := os.Stat(listPath); os.IsNotExist(err) {
			return
		}

		content, err := os.ReadFile(listPath)
		if err != nil {
			return
		}

		lines := strings.Split(string(content), "\n")
		var nodes []string
		nodeRegex := regexp.MustCompile(`(vless|vmess|trojan|hysteria2|tuic)://`)

		for _, line := range lines {
			if nodeRegex.MatchString(line) {
				nodes = append(nodes, line)
			}
		}

		if len(nodes) == 0 {
			return
		}

		data := map[string][]string{"nodes": nodes}
		jsonData, _ := json.Marshal(data)

		resp, err := http.Post(cfg.UploadURL+"/api/add-nodes", "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			log.Println("节点上传成功")
		}
	}
}

func downloadMonitorScript() bool {
	if cfg.MonitorKey == "" || cfg.MonitorServer == "" || cfg.MonitorURL == "" {
		log.Println("监控环境变量不完整，跳过监控脚本启动")
		return false
	}

	monitorURL := "https://raw.githubusercontent.com/mimaldq/cf-vps-monitor/main/cf-vps-monitor.sh"
	monitorPath := filepath.Join(cfg.FilePath, fileNames["monitor"])

	log.Printf("从 %s 下载监控脚本", monitorURL)

	resp, err := http.Get(monitorURL)
	if err != nil {
		log.Printf("下载监控脚本失败: %v", err)
		return false
	}
	defer resp.Body.Close()

	out, err := os.Create(monitorPath)
	if err != nil {
		log.Printf("创建监控脚本文件失败: %v", err)
		return false
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		log.Printf("写入监控脚本文件失败: %v", err)
		return false
	}

	if err := os.Chmod(monitorPath, 0755); err != nil {
		log.Printf("设置监控脚本权限失败: %v", err)
		return false
	}

	log.Println("监控脚本下载完成")
	return true
}

func runMonitorScript() {
	if cfg.MonitorKey == "" || cfg.MonitorServer == "" || cfg.MonitorURL == "" {
		return
	}

	monitorPath := filepath.Join(cfg.FilePath, fileNames["monitor"])
	args := []string{
		"-i",
		"-k", cfg.MonitorKey,
		"-s", cfg.MonitorServer,
		"-u", cfg.MonitorURL,
	}

	log.Printf("运行监控脚本: %s %s", monitorPath, strings.Join(args, " "))

	monitorCmd = exec.Command(monitorPath, args...)
	go func() {
		if err := monitorCmd.Run(); err != nil {
			log.Printf("监控脚本退出: %v", err)
			log.Println("将在30秒后重启监控脚本...")
			time.Sleep(30 * time.Second)
			runMonitorScript()
		}
	}()
}

func startMonitorScript() {
	if cfg.MonitorKey == "" || cfg.MonitorServer == "" || cfg.MonitorURL == "" {
		log.Println("监控脚本未配置，跳过")
		return
	}

	time.Sleep(10 * time.Second)

	if downloaded := downloadMonitorScript(); downloaded {
		runMonitorScript()
	}
}

func AddVisitTask() {
	if !cfg.AutoAccess || cfg.ProjectURL == "" {
		log.Println("跳过添加自动访问任务")
		return
	}

	data := map[string]string{"url": cfg.ProjectURL}
	jsonData, _ := json.Marshal(data)

	resp, err := http.Post("https://oooo.serv00.net/add-url", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("添加自动访问任务失败: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		log.Println("自动访问任务添加成功")
	} else {
		log.Println("添加自动访问任务失败")
	}
}

func startServer() {
	log.Println("开始服务器初始化...")

	deleteNodes()
	cleanupOldFiles()

	argoType()
	generateConfig()
	downloadFilesAndRun()

	log.Println("等待隧道启动...")
	time.Sleep(5 * time.Second)

	extractDomains()
	AddVisitTask()

	log.Println("服务器初始化完成")

	// 90秒后清理文件
	time.Sleep(90 * time.Second)
	cleanFiles()
	log.Println("应用正在运行")
	log.Println("感谢使用此脚本，享受吧！")
}

func cleanFiles() {
	filesToDelete := []string{
		filepath.Join(cfg.FilePath, "boot.log"),
		filepath.Join(cfg.FilePath, "config.json"),
		filepath.Join(cfg.FilePath, fileNames["web"]),
		filepath.Join(cfg.FilePath, fileNames["bot"]),
		filepath.Join(cfg.FilePath, fileNames["monitor"]),
	}

	if cfg.NezhaPort != "" {
		filesToDelete = append(filesToDelete, filepath.Join(cfg.FilePath, fileNames["npm"]))
	} else if cfg.NezhaServer != "" && cfg.NezhaKey != "" {
		filesToDelete = append(filesToDelete, filepath.Join(cfg.FilePath, fileNames["php"]))
	}

	for _, file := range filesToDelete {
		if _, err := os.Stat(file); err == nil {
			os.Remove(file)
		}
	}
}
