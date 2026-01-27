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
	xrayProxy       *httputil.ReverseProxy
	httpProxy       *httputil.ReverseProxy
	tunnelProcess   *exec.Cmd
	tunnelMutex     sync.Mutex
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
		ArgoDomain:    getEnv("ARGO_DOMAIN", ""),
		ArgoAuth:      getEnv("ARGO_AUTH", ""),
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
				http.Error(w, "Xray代理错误", http.StatusInternalServerError)
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
				http.Error(w, "HTTP代理错误", http.StatusInternalServerError)
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

// 主HTTP处理函数
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
	
	// 其他请求处理
	http.NotFound(w, r)
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
	
	go func() {
		http.Post(config.UploadURL+"/api/delete-nodes", "application/json", bytes.NewBuffer(jsonData))
	}()
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
	
	jsonData, err := json.MarshalIndent(configData, "", "  ")
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
			// 哪吒v1
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
uuid: %s
`, config.NezhaKey, config.NezhaServer, nezhatls, config.UUID)
			
			yamlPath := filepath.Join(filePath, "config.yaml")
			if err := os.WriteFile(yamlPath, []byte(configYaml), 0644); err != nil {
				log.Printf("写入哪吒配置失败: %v", err)
			}
			
			// 运行哪吒v1
			cmd := exec.Command(phpPath, "-c", yamlPath)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				log.Printf("运行哪吒v1失败: %v", err)
			} else {
				log.Printf("%s 运行中", phpName)
				go cmd.Wait()
			}
			
			time.Sleep(1 * time.Second)
		} else {
			// 哪吒v0
			args := []string{
				"-s", fmt.Sprintf("%s:%s", config.NezhaServer, config.NezhaPort),
				"-p", config.NezhaKey,
			}
			
			tlsPorts := []string{"443", "8443", "2096", "2087", "2083", "2053"}
			for _, port := range tlsPorts {
				if port == config.NezhaPort {
					args = append(args, "--tls")
					break
				}
			}
			
			args = append(args, "--disable-auto-update", "--report-delay", "4", "--skip-conn", "--skip-procs")
			
			cmd := exec.Command(npmPath, args...)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				log.Printf("运行哪吒v0失败: %v", err)
			} else {
				log.Printf("%s 运行中", npmName)
				go cmd.Wait()
			}
			
			time.Sleep(1 * time.Second)
		}
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
		} else if config.ArgoAuth != "" && strings.Contains(config.ArgoAuth, "TunnelSecret") {
			// 隧道配置文件
			if _, err := os.Stat(tunnelYamlPath); os.IsNotExist(err) {
				log.Println("等待隧道配置文件生成...")
				time.Sleep(2 * time.Second)
			}
			args = []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate",
				"--protocol", "http2", "--config", tunnelYamlPath, "run"}
		} else {
			// 临时隧道 - 使用debug级别日志以便查看更多信息
			args = []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate",
				"--protocol", "http2", "--logfile", bootLogPath, "--loglevel", "debug",
				"--url", fmt.Sprintf("http://localhost:%s", config.ArgoPort)}
		}
		
		tunnelMutex.Lock()
		cmd := exec.Command(botPath, args...)
		tunnelProcess = cmd
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			log.Printf("运行cloudflared失败: %v", err)
		} else {
			log.Printf("%s 运行中", botName)
			go func() {
				err := cmd.Wait()
				log.Printf("隧道进程退出: %v", err)
				// 如果隧道进程退出，尝试重新启动
				time.Sleep(10 * time.Second)
				restartTunnel()
			}()
		}
		tunnelMutex.Unlock()
		
		// 增加初始等待时间，让隧道有时间启动
		log.Println("等待隧道启动和上线（这可能需要15-30秒）...")
		time.Sleep(15 * time.Second)
	}
	
	time.Sleep(3 * time.Second)
	return nil
}

// 重启隧道
func restartTunnel() {
	tunnelMutex.Lock()
	defer tunnelMutex.Unlock()
	
	log.Println("正在重新启动隧道...")
	
	// 杀死现有进程
	killBotProcess()
	time.Sleep(3 * time.Second)
	
	// 重新启动
	if config.ArgoAuth != "" && len(config.ArgoAuth) >= 120 && len(config.ArgoAuth) <= 250 &&
	   strings.Contains(config.ArgoAuth, "=") {
		args := []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate",
			"--protocol", "http2", "run", "--token", config.ArgoAuth}
		cmd := exec.Command(botPath, args...)
		tunnelProcess = cmd
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			log.Printf("重新启动隧道失败: %v", err)
		} else {
			log.Println("隧道已重新启动")
			go cmd.Wait()
		}
	} else {
		// 临时隧道
		args := []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate",
			"--protocol", "http2", "--logfile", bootLogPath, "--loglevel", "debug",
			"--url", fmt.Sprintf("http://localhost:%s", config.ArgoPort)}
		cmd := exec.Command(botPath, args...)
		tunnelProcess = cmd
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			log.Printf("重新启动隧道失败: %v", err)
		} else {
			log.Println("隧道已重新启动")
			go cmd.Wait()
		}
	}
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
		
		var tunnelConfig map[string]interface{}
		if err := json.Unmarshal([]byte(config.ArgoAuth), &tunnelConfig); err != nil {
			log.Printf("解析隧道配置失败: %v", err)
			return
		}
		
		tunnelID, ok := tunnelConfig["TunnelID"].(string)
		if !ok {
			log.Println("隧道配置中缺少TunnelID")
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
  - service: http_status:404
`, tunnelID, tunnelJsonPath, config.ArgoDomain, config.ArgoPort)
		
		if err := os.WriteFile(tunnelYamlPath, []byte(tunnelYaml), 0644); err != nil {
			log.Printf("写入隧道YAML配置失败: %v", err)
			return
		}
		
		log.Println("隧道YAML配置生成成功")
	} else {
		log.Println("ARGO_AUTH 不是TunnelSecret格式，使用token连接隧道")
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
	
	time.Sleep(30 * time.Second)
	
	if downloaded := downloadMonitorScript(); downloaded {
		runMonitorScript()
	}
}

// 提取临时隧道域名 - 增加重试机制
func extractDomains() error {
	var argoDomain string
	
	if config.ArgoAuth != "" && config.ArgoDomain != "" {
		argoDomain = config.ArgoDomain
		log.Printf("使用固定域名: %s", argoDomain)
		return generateLinks(argoDomain)
	}
	
	// 尝试读取日志文件，最多重试10次
	maxRetries := 10
	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Printf("尝试读取隧道日志 (第 %d/%d 次)...", attempt, maxRetries)
		
		data, err := os.ReadFile(bootLogPath)
		if err != nil {
			log.Printf("读取日志文件失败: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}
		
		lines := strings.Split(string(data), "\n")
		foundDomain := false
		for _, line := range lines {
			if strings.Contains(line, "trycloudflare.com") {
				// 查找域名
				start := strings.Index(line, "https://")
				if start == -1 {
					start = strings.Index(line, "http://")
				}
				if start != -1 {
					line = line[start:]
					end := strings.Index(line, " ")
					if end == -1 {
						end = len(line)
					}
					domain := line[:end]
					if strings.Contains(domain, "trycloudflare.com") {
						argoDomain = strings.TrimPrefix(domain, "https://")
						argoDomain = strings.TrimPrefix(argoDomain, "http://")
						argoDomain = strings.TrimSuffix(argoDomain, "/")
						log.Printf("成功找到临时域名: %s", argoDomain)
						foundDomain = true
						break
					}
				}
			}
			// 检查隧道状态
			if strings.Contains(line, "INF Registered tunnel connection") {
				log.Printf("隧道连接已注册")
			}
			if strings.Contains(line, "INF Connection") {
				log.Printf("隧道连接信息: %s", line)
			}
		}
		
		if foundDomain {
			return generateLinks(argoDomain)
		}
		
		if attempt < maxRetries {
			waitTime := time.Duration(attempt) * 2 * time.Second
			log.Printf("未找到域名，等待 %v 后重试...", waitTime)
			time.Sleep(waitTime)
		}
	}
	
	log.Println("经过多次尝试仍未找到域名，将稍后重试...")
	// 不立即重启，等待健康检查处理
	return fmt.Errorf("未能提取到隧道域名")
}

// 重新启动隧道并尝试提取域名
func restartTunnelAndExtract() error {
	log.Println("重新启动隧道以获取域名...")
	
	// 停止现有的cloudflared进程
	killBotProcess()
	time.Sleep(5 * time.Second)
	
	// 删除旧的日志文件
	os.Remove(bootLogPath)
	
	// 重新启动cloudflared（临时隧道）
	args := []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate",
		"--protocol", "http2", "--logfile", bootLogPath, "--loglevel", "debug",
		"--url", fmt.Sprintf("http://localhost:%s", config.ArgoPort)}
	
	cmd := exec.Command(botPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		log.Printf("重新启动隧道失败: %v", err)
		return err
	}
	tunnelProcess = cmd
	go cmd.Wait()
	
	log.Printf("%s 已重新启动，等待隧道上线...", botName)
	
	// 等待更长时间让隧道上线
	time.Sleep(20 * time.Second)
	
	// 再次尝试提取域名
	return extractDomains()
}

// 杀死bot进程
func killBotProcess() {
	if tunnelProcess != nil && tunnelProcess.Process != nil {
		tunnelProcess.Process.Kill()
	}
	
	if runtime.GOOS == "windows" {
		exec.Command("taskkill", "/f", "/im", botName).Run()
		exec.Command("taskkill", "/f", "/im", "cloudflared").Run()
	} else {
		exec.Command("pkill", "-f", botName).Run()
		exec.Command("pkill", "-f", "cloudflared").Run()
		exec.Command("killall", botName).Run()
		exec.Command("killall", "cloudflared").Run()
	}
	time.Sleep(2 * time.Second)
}

// 获取ISP信息
func getMetaInfo() string {
	client := &http.Client{Timeout: 5 * time.Second}
	
	resp, err := client.Get("https://ipapi.co/json/")
	if err == nil {
		defer resp.Body.Close()
		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			if countryCode, ok := data["country_code"].(string); ok {
				if org, ok := data["org"].(string); ok {
					return fmt.Sprintf("%s_%s", countryCode, org)
				}
			}
		}
	}
	
	resp, err = client.Get("http://ip-api.com/json/")
	if err == nil {
		defer resp.Body.Close()
		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			if status, ok := data["status"].(string); ok && status == "success" {
				if countryCode, ok := data["countryCode"].(string); ok {
					if org, ok := data["org"].(string); ok {
						return fmt.Sprintf("%s_%s", countryCode, org)
					}
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
	
	encodedPath := "%2Fvless-argo%3Fed%3D2560"
	
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
	
	subTxt := fmt.Sprintf(`
vless://%s@%s:%s?encryption=none&security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%s#%s

vmess://%s

trojan://%s@%s:%s?security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%s#%s
`, config.UUID, config.CFIP, config.CFPort, argoDomain, argoDomain, encodedPath, nodeName,
		vmessBase64, config.UUID, config.CFIP, config.CFPort, argoDomain, argoDomain, encodedPath, nodeName)
	
	encoded := base64.StdEncoding.EncodeToString([]byte(subTxt))
	log.Println("生成的订阅内容 (base64):")
	log.Println(encoded)
	
	if err := os.WriteFile(subPath, []byte(encoded), 0644); err != nil {
		return err
	}
	
	log.Printf("%s/sub.txt 保存成功", filePath)
	
	go uploadNodes()
	
	return nil
}

// 上传节点或订阅
func uploadNodes() {
	if config.UploadURL == "" {
		return
	}
	
	if config.ProjectURL != "" {
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
		if _, err := os.Stat(listPath); os.IsNotExist(err) {
			return
		}
		
		data, err := os.ReadFile(listPath)
		if err != nil {
			return
		}
		
		lines := strings.Split(string(data), "\n")
		var nodes []string
		for _, line := range lines {
			if strings.HasPrefix(line, "vless://") || strings.HasPrefix(line, "vmess://") ||
			   strings.HasPrefix(line, "trojan://") || strings.HasPrefix(line, "hysteria2://") ||
			   strings.HasPrefix(line, "tuic://") {
				nodes = append(nodes, line)
			}
		}
		
		if len(nodes) == 0 {
			return
		}
		
		payload := map[string][]string{"nodes": nodes}
		jsonData, _ := json.Marshal(payload)
		
		go func() {
			http.Post(config.UploadURL+"/api/add-nodes", "application/json", bytes.NewBuffer(jsonData))
		}()
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

// 隧道健康检查
func tunnelHealthCheck() {
	// 等待3分钟后开始定期检查隧道状态
	time.Sleep(3 * time.Minute)
	
	for {
		// 每5分钟检查一次
		time.Sleep(5 * time.Minute)
		
		log.Println("执行隧道健康检查...")
		
		// 检查隧道日志文件是否存在
		if _, err := os.Stat(bootLogPath); os.IsNotExist(err) {
			log.Println("隧道日志文件不存在，隧道可能已断开，尝试重新启动...")
			restartTunnel()
			continue
		}
		
		// 检查日志文件中是否有错误信息
		data, err := os.ReadFile(bootLogPath)
		if err == nil {
			content := string(data)
			
			// 检查是否包含域名
			if strings.Contains(content, "trycloudflare.com") {
				log.Println("隧道正常运行，已检测到域名")
			} else if strings.Contains(content, "ERR") && 
			   (strings.Contains(content, "connection failed") || 
			    strings.Contains(content, "disconnected") ||
				strings.Contains(content, "failed to connect")) {
				log.Println("检测到隧道连接错误，尝试重新连接...")
				restartTunnel()
			} else {
				// 检查隧道是否活跃
				lines := strings.Split(content, "\n")
				recentLogs := lines[len(lines)-10:] // 查看最近10行日志
				hasRecentActivity := false
				for _, line := range recentLogs {
					if strings.Contains(line, "INF") && 
					   (strings.Contains(line, "request") || 
					    strings.Contains(line, "connection")) {
						hasRecentActivity = true
						break
					}
				}
				
				if !hasRecentActivity {
					log.Println("隧道长时间无活动，尝试重新启动...")
					restartTunnel()
				}
			}
		}
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
	
	// 等待更长的时间让隧道完全上线
	log.Println("等待隧道完全上线（可能需要1-2分钟）...")
	time.Sleep(30 * time.Second)
	
	// 提取域名（内部已有重试机制）
	if err := extractDomains(); err != nil {
		log.Printf("提取域名失败: %v", err)
		// 即使失败也继续运行，健康检查会稍后处理
		log.Println("域名提取失败，程序将继续运行，健康检查会稍后重试")
	}
	
	addVisitTask()
	log.Println("服务器初始化完成")
	log.Println("注意：临时隧道可能需要几分钟才能完全上线")
	log.Println("请耐心等待，健康检查会自动维护隧道连接")
}

func main() {
	initConfig()
	
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/", mainHandler)
	
	internalServer := &http.Server{
		Addr:    ":" + config.Port,
		Handler: httpMux,
	}
	
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
	}
	
	go func() {
		log.Printf("HTTP服务运行在内部端口: %s", config.Port)
		if err := internalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP服务器错误: %v", err)
		}
	}()
	
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
	
	// 启动隧道健康检查
	go tunnelHealthCheck()
	
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
	
	// 停止隧道进程
	killBotProcess()
	
	// 优雅关闭服务器
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	internalServer.Shutdown(ctx)
	proxyServer.Shutdown(ctx)
	
	log.Println("程序退出")
}
