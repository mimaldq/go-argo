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
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
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
	httpProxy       *httputil.ReverseProxy
)

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

// 主处理函数
func mainHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" && r.URL.Path == "/" {
		// 检查index.html是否存在
		if _, err := os.Stat("index.html"); err == nil {
			http.ServeFile(w, r, "index.html")
		} else {
			fmt.Fprint(w, "Hello world!")
		}
		return
	}

	if r.Method == "GET" && r.URL.Path == "/"+config.SubPath {
		// 提供订阅
		if data, err := os.ReadFile(subPath); err == nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write(data)
		} else {
			http.NotFound(w, r)
		}
		return
	}

	// 代理处理
	proxyHandler(w, r)
}

// 代理处理器
func proxyHandler(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path

	if strings.HasPrefix(urlPath, "/vless-argo") ||
		strings.HasPrefix(urlPath, "/vmess-argo") ||
		strings.HasPrefix(urlPath, "/trojan-argo") ||
		urlPath == "/vless" ||
		urlPath == "/vmess" ||
		urlPath == "/trojan" {
		// 转发到Xray
		if httpProxy == nil {
			xrayURL, _ := url.Parse("http://localhost:3001")
			httpProxy = httputil.NewSingleHostReverseProxy(xrayURL)
		}
		httpProxy.ServeHTTP(w, r)
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

// WebSocket处理器
func wsHandler(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path

	if strings.HasPrefix(urlPath, "/vless-argo") ||
		strings.HasPrefix(urlPath, "/vmess-argo") ||
		strings.HasPrefix(urlPath, "/trojan-argo") {
		// 转发到Xray的WebSocket
		director := func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "localhost:3001"
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

// 删除历史节点
func deleteNodes() {
	if config.UploadURL == "" {
		return
	}

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
	for _, line := range lines {
		if strings.Contains(line, "vless://") || strings.Contains(line, "vmess://") ||
			strings.Contains(line, "trojan://") || strings.Contains(line, "hysteria2://") ||
			strings.Contains(line, "tuic://") {
			nodes = append(nodes, line)
		}
	}

	if len(nodes) == 0 {
		return
	}

	payload := map[string][]string{"nodes": nodes}
	jsonData, _ := json.Marshal(payload)

	http.Post(config.UploadURL+"/api/delete-nodes", "application/json", bytes.NewBuffer(jsonData))
}

// 清理旧文件
func cleanupOldFiles() {
	files, err := os.ReadDir(filePath)
	if err != nil {
		return
	}

	for _, file := range files {
		filePath := filepath.Join(filePath, file.Name())
		os.Remove(filePath)
	}
}

// 生成Xray配置文件
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
				"port": 3001,
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{
						{"id": config.UUID, "flow": "xtls-rprx-vision"},
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
					"clients": []map[string]interface{}{
						{"id": config.UUID},
					},
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
					"clients": []map[string]interface{}{
						{"id": config.UUID, "level": 0},
					},
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network":  "ws",
					"security": "none",
					"wsSettings": map[string]interface{}{
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
						{"id": config.UUID, "alterId": 0},
					},
				},
				"streamSettings": map[string]interface{}{
					"network": "ws",
					"wsSettings": map[string]interface{}{
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
					"clients": []map[string]interface{}{
						{"password": config.UUID},
					},
				},
				"streamSettings": map[string]interface{}{
					"network":  "ws",
					"security": "none",
					"wsSettings": map[string]interface{}{
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
		},
	}

	jsonData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, jsonData, 0644)
}

// 获取系统架构
func getSystemArchitecture() string {
	arch := runtime.GOARCH
	if arch == "arm" || arch == "arm64" {
		return "arm"
	}
	return "amd"
}

// 下载文件
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
		os.Remove(filePath)
		return err
	}

	if err := os.Chmod(filePath, 0755); err != nil {
		return err
	}

	log.Printf("下载 %s 成功", filepath.Base(filePath))
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

	if architecture == "arm" {
		files = []struct {
			fileName string
			fileURL  string
		}{
			{webPath, "https://arm64.ssss.nyc.mn/web"},
			{botPath, "https://arm64.ssss.nyc.mn/bot"},
		}
	} else {
		files = []struct {
			fileName string
			fileURL  string
		}{
			{webPath, "https://amd64.ssss.nyc.mn/web"},
			{botPath, "https://amd64.ssss.nyc.mn/bot"},
		}
	}

	if config.NezhaServer != "" && config.NezhaKey != "" {
		if config.NezhaPort != "" {
			var npmURL string
			if architecture == "arm" {
				npmURL = "https://arm64.ssss.nyc.mn/agent"
			} else {
				npmURL = "https://amd64.ssss.nyc.mn/agent"
			}
			files = append([]struct {
				fileName string
				fileURL  string
			}{{npmPath, npmURL}}, files...)
		} else {
			var phpURL string
			if architecture == "arm" {
				phpURL = "https://arm64.ssss.nyc.mn/v1"
			} else {
				phpURL = "https://amd64.ssss.nyc.mn/v1"
			}
			files = append([]struct {
				fileName string
				fileURL  string
			}{{phpPath, phpURL}}, files...)
		}
	}

	return files
}

// 下载并运行依赖文件
func downloadFilesAndRun() error {
	architecture := getSystemArchitecture()
	files := getFilesForArchitecture(architecture)

	if len(files) == 0 {
		return fmt.Errorf("无法找到适合当前架构的文件")
	}

	// 下载文件
	for _, file := range files {
		if err := downloadFile(file.fileName, file.fileURL); err != nil {
			log.Printf("下载文件失败: %v", err)
			continue
		}
	}

	// 运行哪吒监控
	if config.NezhaServer != "" && config.NezhaKey != "" {
		if config.NezhaPort == "" {
			// 哪吒v1 - 简化处理
			cmd := exec.Command(phpPath)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				log.Printf("运行哪吒v1失败: %v", err)
			} else {
				log.Printf("%s 运行中", phpName)
				go cmd.Wait()
			}
		} else {
			// 哪吒v0
			args := []string{
				"-s", fmt.Sprintf("%s:%s", config.NezhaServer, config.NezhaPort),
				"-p", config.NezhaKey,
				"--disable-auto-update", "--report-delay", "4", "--skip-conn", "--skip-procs",
			}

			cmd := exec.Command(npmPath, args...)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				log.Printf("运行哪吒v0失败: %v", err)
			} else {
				log.Printf("%s 运行中", npmName)
				go cmd.Wait()
			}
		}
		time.Sleep(1 * time.Second)
	} else {
		log.Println("哪吒监控变量为空，跳过运行")
	}

	// 运行Xray
	cmd := exec.Command(webPath, "-c", configPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		log.Printf("运行Xray失败: %v", err)
	} else {
		log.Printf("%s 运行中", webName)
		go cmd.Wait()
	}

	time.Sleep(1 * time.Second)

	// 运行cloudflared
	if _, err := os.Stat(botPath); err == nil {
		var args []string

		if config.ArgoAuth != "" && len(config.ArgoAuth) >= 120 && len(config.ArgoAuth) <= 250 &&
			strings.Contains(config.ArgoAuth, "=") {
			// Token认证
			args = []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate",
				"--protocol", "http2", "run", "--token", config.ArgoAuth}
		} else {
			// 临时隧道
			args = []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate",
				"--protocol", "http2", "--logfile", bootLogPath, "--loglevel", "info",
				"--url", fmt.Sprintf("http://localhost:%s", config.ArgoPort)}
		}

		cmd := exec.Command(botPath, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			log.Printf("运行cloudflared失败: %v", err)
		} else {
			log.Printf("%s 运行中", botName)
			go cmd.Wait()
		}

		// 等待隧道启动
		log.Println("等待隧道启动...")
		time.Sleep(5 * time.Second)
	}

	time.Sleep(2 * time.Second)
	return nil
}

// 处理固定隧道
func argoType() {
	if config.ArgoAuth == "" || config.ArgoDomain == "" {
		log.Println("ARGO_DOMAIN 或 ARGO_AUTH 为空，使用快速隧道")
		return
	}

	if strings.Contains(config.ArgoAuth, "TunnelSecret") {
		if err := os.WriteFile(tunnelJsonPath, []byte(config.ArgoAuth), 0644); err != nil {
			log.Printf("写入隧道JSON配置失败: %v", err)
			return
		}

		// 简化处理，不解析JSON
		tunnelYaml := fmt.Sprintf(`
credentials-file: %s
protocol: http2
`, tunnelJsonPath)

		if err := os.WriteFile(tunnelYamlPath, []byte(tunnelYaml), 0644); err != nil {
			log.Printf("写入隧道YAML配置失败: %v", err)
			return
		}

		log.Println("隧道配置生成成功")
	} else {
		log.Println("使用token连接隧道")
	}
}

// 下载监控脚本
func downloadMonitorScript() bool {
	if config.MonitorKey == "" || config.MonitorServer == "" || config.MonitorURL == "" {
		log.Println("监控环境变量不完整，跳过监控脚本启动")
		return false
	}

	monitorURL := "https://raw.githubusercontent.com/mimaldq/cf-vps-monitor/main/cf-vps-monitor.sh"
	log.Printf("从 %s 下载监控脚本", monitorURL)

	resp, err := http.Get(monitorURL)
	if err != nil {
		log.Printf("下载监控脚本失败: %v", err)
		return false
	}
	defer resp.Body.Close()

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

	args := []string{
		"-i",
		"-k", config.MonitorKey,
		"-s", config.MonitorServer,
		"-u", config.MonitorURL,
	}

	log.Printf("运行监控脚本: %s %s", monitorPath, strings.Join(args, " "))

	cmd := exec.Command(monitorPath, args...)
	monitorProcess = cmd

	go func() {
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("监控脚本退出，错误: %v", err)
			log.Println("将在30秒后重启监控脚本...")
			time.Sleep(30 * time.Second)
			runMonitorScript()
		} else {
			log.Printf("监控脚本输出: %s", output)
		}
	}()

	if err := cmd.Start(); err != nil {
		log.Printf("启动监控脚本失败: %v", err)
		return
	}
}

// 启动监控脚本
func startMonitorScript() {
	if config.MonitorKey == "" || config.MonitorServer == "" || config.MonitorURL == "" {
		log.Println("监控脚本未配置，跳过")
		return
	}

	// 等待其他服务启动
	time.Sleep(10 * time.Second)

	if downloaded := downloadMonitorScript(); downloaded {
		runMonitorScript()
	}
}

// 提取临时隧道域名
func extractDomains() error {
	var argoDomain string

	if config.ArgoAuth != "" && config.ArgoDomain != "" {
		argoDomain = config.ArgoDomain
		log.Printf("使用固定域名: %s", argoDomain)
		return generateLinks(argoDomain)
	}

	// 读取日志文件获取临时域名
	data, err := os.ReadFile(bootLogPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.Contains(line, "trycloudflare.com") {
			// 简化域名提取
			parts := strings.Split(line, " ")
			for _, part := range parts {
				if strings.Contains(part, "trycloudflare.com") {
					argoDomain = strings.TrimPrefix(part, "https://")
					argoDomain = strings.TrimSuffix(argoDomain, "/")
					log.Printf("找到临时域名: %s", argoDomain)
					return generateLinks(argoDomain)
				}
			}
		}
	}

	log.Println("未找到域名")
	return fmt.Errorf("未找到域名")
}

// 获取ISP信息
func getMetaInfo() string {
	client := &http.Client{Timeout: 3 * time.Second}

	// 尝试获取IP信息
	resp, err := client.Get("https://ipinfo.io/json")
	if err == nil {
		defer resp.Body.Close()
		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			if country, ok := data["country"].(string); ok {
				if org, ok := data["org"].(string); ok {
					return fmt.Sprintf("%s_%s", country, org)
				}
			}
		}
	}

	return "Unknown"
}

// 生成订阅链接
func generateLinks(argoDomain string) error {
	isp := getMetaInfo()
	nodeName := isp
	if config.Name != "" {
		nodeName = fmt.Sprintf("%s-%s", config.Name, isp)
	}

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

	vmessJSON, _ := json.Marshal(vmessConfig)
	vmessBase64 := base64.StdEncoding.EncodeToString(vmessJSON)

	// 生成订阅文本
	subTxt := fmt.Sprintf(`
vless://%s@%s:%s?encryption=none&security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=/vless-argo?ed=2560#%s

vmess://%s

trojan://%s@%s:%s?security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=/trojan-argo?ed=2560#%s
`, config.UUID, config.CFIP, config.CFPort, argoDomain, argoDomain, nodeName,
		vmessBase64, config.UUID, config.CFIP, config.CFPort, argoDomain, argoDomain, nodeName)

	// 打印到控制台
	encoded := base64.StdEncoding.EncodeToString([]byte(subTxt))
	log.Println(encoded)

	// 保存文件
	if err := os.WriteFile(subPath, []byte(encoded), 0644); err != nil {
		return err
	}

	log.Printf("%s/sub.txt 保存成功", filePath)

	// 上传节点
	go uploadNodes()

	return nil
}

// 上传节点或订阅
func uploadNodes() {
	if config.UploadURL == "" {
		return
	}

	if config.ProjectURL != "" {
		// 上传订阅
		subscriptionUrl := fmt.Sprintf("%s/%s", config.ProjectURL, config.SubPath)
		payload := map[string][]string{
			"subscription": {subscriptionUrl},
		}

		jsonData, _ := json.Marshal(payload)

		resp, err := http.Post(config.UploadURL+"/api/add-subscriptions",
			"application/json", bytes.NewBuffer(jsonData))
		if err == nil && resp.StatusCode == 200 {
			log.Println("订阅上传成功")
		} else {
			log.Printf("订阅上传失败: %v", err)
		}
	} else {
		// 上传节点
		if _, err := os.Stat(listPath); os.IsNotExist(err) {
			return
		}
	}
}

// 清理文件
func cleanFiles() {
	time.AfterFunc(90*time.Second, func() {
		filesToDelete := []string{
			bootLogPath, configPath, webPath, botPath, monitorPath,
		}

		if config.NezhaPort != "" {
			filesToDelete = append(filesToDelete, npmPath)
		} else if config.NezhaServer != "" && config.NezhaKey != "" {
			filesToDelete = append(filesToDelete, phpPath)
		}

		for _, file := range filesToDelete {
			os.Remove(file)
		}

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

	payload := map[string]string{"url": config.ProjectURL}
	jsonData, _ := json.Marshal(payload)

	resp, err := http.Post("https://oooo.serv00.net/add-url",
		"application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("添加自动访问任务失败: %v", err)
		return
	}
	defer resp.Body.Close()

	log.Println("自动访问任务添加成功")
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
	// 初始化配置
	initConfig()

	// 创建HTTP服务器
	mux := http.NewServeMux()
	mux.HandleFunc("/", mainHandler)

	// 创建内部HTTP服务器
	internalServer := &http.Server{
		Addr:    ":" + config.Port,
		Handler: mux,
	}

	// 创建外部代理服务器
	proxyServer := &http.Server{
		Addr: ":" + config.ArgoPort,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// WebSocket升级处理
			if r.Header.Get("Upgrade") == "websocket" {
				wsHandler(w, r)
			} else {
				mainHandler(w, r)
			}
		}),
	}

	// 启动内部HTTP服务器
	go func() {
		log.Printf("HTTP服务运行在内部端口: %s", config.Port)
		if err := internalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP服务器错误: %v", err)
		}
	}()

	// 启动外部代理服务器
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
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Println("收到关闭信号，正在清理...")

	// 停止监控脚本
	if monitorProcess != nil {
		log.Println("停止监控脚本...")
		monitorProcess.Process.Kill()
	}

	// 优雅关闭服务器
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	internalServer.Shutdown(ctx)
	proxyServer.Shutdown(ctx)

	log.Println("程序退出")
}
