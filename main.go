package main

import (
	"bytes"
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
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// 环境变量配置结构体
type Config struct {
	UploadURL     string `env:"UPLOAD_URL"`
	ProjectURL    string `env:"PROJECT_URL"`
	AutoAccess    bool   `env:"AUTO_ACCESS"`
	FilePath      string `env:"FILE_PATH" default:"./tmp"`
	SubPath       string `env:"SUB_PATH" default:"sub"`
	Port          string `env:"SERVER_PORT" default:"3000"`
	UUID          string `env:"UUID" default:"e2cae6af-5cdd-fa48-4137-ad3e617fbab0"`
	NezhaServer   string `env:"NEZHA_SERVER"`
	NezhaPort     string `env:"NEZHA_PORT"`
	NezhaKey      string `env:"NEZHA_KEY"`
	ArgoDomain    string `env:"ARGO_DOMAIN" default:"date.goyo123.ggff.net"`
	ArgoAuth      string `env:"ARGO_AUTH" default:"eyJhIjoiNWRmNTFlZjhhMTNiMWQ1ZDFhODhhZTAxNWFmYTU5OGIiLCJ0IjoiM2Q0M2I5ZTgtNDM0Zi00YjA2LTk5ZmEtMjc2ODc0MGI3ZTcyIiwicyI6Ill6SmhNemxoT1RFdFpUSTROeTAwTmpFeUxUazBOelV0WlRZNFptRTFabUV6WldKbCJ9"`
	ArgoPort      string `env:"ARGO_PORT" default:"7860"`
	CFIP          string `env:"CFIP" default:"cdns.doon.eu.org"`
	CFPort        string `env:"CFPORT" default:"443"`
	Name          string `env:"NAME"`
	MonitorKey    string `env:"MONITOR_KEY"`
	MonitorServer string `env:"MONITOR_SERVER"`
	MonitorURL    string `env:"MONITOR_URL"`
}

// Xray配置文件结构体
type XrayConfig struct {
	Log struct {
		Access   string `json:"access"`
		Error    string `json:"error"`
		Loglevel string `json:"loglevel"`
	} `json:"log"`
	DNS struct {
		Servers       []string `json:"servers"`
		QueryStrategy string   `json:"queryStrategy"`
		DisableCache  bool     `json:"disableCache"`
	} `json:"dns"`
	Inbounds  []Inbound  `json:"inbounds"`
	Outbounds []Outbound `json:"outbounds"`
	Routing   Routing    `json:"routing"`
}

type Inbound struct {
	Port           int              `json:"port"`
	Listen         string           `json:"listen,omitempty"`
	Protocol       string           `json:"protocol"`
	Settings       InboundSettings  `json:"settings"`
	StreamSettings StreamSettings   `json:"streamSettings"`
	Sniffing       *Sniffing        `json:"sniffing,omitempty"`
}

type InboundSettings struct {
	Clients    []Client `json:"clients"`
	Decryption string   `json:"decryption,omitempty"`
	Fallbacks  []Fallback `json:"fallbacks,omitempty"`
}

type Client struct {
	ID     string `json:"id"`
	Flow   string `json:"flow,omitempty"`
	Level  int    `json:"level,omitempty"`
	AlterID int   `json:"alterId,omitempty"`
	Password string `json:"password,omitempty"`
}

type Fallback struct {
	Dest int    `json:"dest"`
	Path string `json:"path,omitempty"`
}

type StreamSettings struct {
	Network   string            `json:"network"`
	Security  string            `json:"security,omitempty"`
	WSSettings *WSSettings      `json:"wsSettings,omitempty"`
}

type WSSettings struct {
	Path string `json:"path"`
}

type Sniffing struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"destOverride"`
	MetadataOnly bool     `json:"metadataOnly"`
}

type Outbound struct {
	Protocol string      `json:"protocol"`
	Tag      string      `json:"tag"`
	Settings interface{} `json:"settings"`
}

type Routing struct {
	DomainStrategy string        `json:"domainStrategy"`
	Rules          []interface{} `json:"rules"`
}

// 全局变量
var (
	cfg        Config
	filePaths  map[string]string
	processes  []*os.Process
	proxy      *httputil.ReverseProxy
	upgrader   = websocket.Upgrader{}
)

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

// 初始化配置
func initConfig() {
	// 从环境变量读取配置
	cfg = Config{
		UploadURL:     os.Getenv("UPLOAD_URL"),
		ProjectURL:    os.Getenv("PROJECT_URL"),
		AutoAccess:    os.Getenv("AUTO_ACCESS") == "true",
		FilePath:      getEnv("FILE_PATH", "./tmp"),
		SubPath:       getEnv("SUB_PATH", "sub"),
		Port:          getEnv("SERVER_PORT", getEnv("PORT", "3000")),
		UUID:          getEnv("UUID", "e2cae6af-5cdd-fa48-4137-ad3e617fbab0"),
		NezhaServer:   os.Getenv("NEZHA_SERVER"),
		NezhaPort:     os.Getenv("NEZHA_PORT"),
		NezhaKey:      os.Getenv("NEZHA_KEY"),
		ArgoDomain:    getEnv("ARGO_DOMAIN", "date.goyo123.ggff.net"),
		ArgoAuth:      getEnv("ARGO_AUTH", "eyJhIjoiNWRmNTFlZjhhMTNiMWQ1ZDFhODhhZTAxNWFmYTU5OGIiLCJ0IjoiM2Q0M2I5ZTgtNDM0Zi00YjA2LTk5ZmEtMjc2ODc0MGI3ZTcyIiwicyI6Ill6SmhNemxoT1RFdFpUSTROeTAwTmpFeUxUazBOelV0WlRZNFptRTFabUV6WldKbCJ9"),
		ArgoPort:      getEnv("ARGO_PORT", "7860"),
		CFIP:          getEnv("CFIP", "cdns.doon.eu.org"),
		CFPort:        getEnv("CFPORT", "443"),
		Name:          os.Getenv("NAME"),
		MonitorKey:    os.Getenv("MONITOR_KEY"),
		MonitorServer: os.Getenv("MONITOR_SERVER"),
		MonitorURL:    os.Getenv("MONITOR_URL"),
	}

	// 创建运行文件夹
	if err := os.MkdirAll(cfg.FilePath, 0755); err != nil {
		log.Fatal("创建文件夹失败:", err)
	}

	// 初始化文件路径
	filePaths = map[string]string{
		"npm":      filepath.Join(cfg.FilePath, generateRandomName()),
		"web":      filepath.Join(cfg.FilePath, generateRandomName()),
		"bot":      filepath.Join(cfg.FilePath, generateRandomName()),
		"php":      filepath.Join(cfg.FilePath, generateRandomName()),
		"monitor":  filepath.Join(cfg.FilePath, "cf-vps-monitor.sh"),
		"sub":      filepath.Join(cfg.FilePath, "sub.txt"),
		"list":     filepath.Join(cfg.FilePath, "list.txt"),
		"bootLog":  filepath.Join(cfg.FilePath, "boot.log"),
		"config":   filepath.Join(cfg.FilePath, "config.json"),
		"tunnelJson": filepath.Join(cfg.FilePath, "tunnel.json"),
		"tunnelYaml": filepath.Join(cfg.FilePath, "tunnel.yml"),
		"nezhaConfig": filepath.Join(cfg.FilePath, "config.yaml"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// 清理历史文件
func cleanupOldFiles() {
	files, err := os.ReadDir(cfg.FilePath)
	if err != nil {
		return
	}

	for _, file := range files {
		if !file.IsDir() {
			os.Remove(filepath.Join(cfg.FilePath, file.Name()))
		}
	}
}

// 生成Xray配置文件
func generateXrayConfig() error {
	config := XrayConfig{}

	// 日志配置
	config.Log.Access = "/dev/null"
	config.Log.Error = "/dev/null"
	config.Log.Loglevel = "none"

	// DNS配置
	config.DNS.Servers = []string{
		"https+local://8.8.8.8/dns-query",
		"https+local://1.1.1.1/dns-query",
		"8.8.8.8",
		"1.1.1.1",
	}
	config.DNS.QueryStrategy = "UseIP"
	config.DNS.DisableCache = false

	// Inbounds配置
	config.Inbounds = []Inbound{
		{
			Port:     3001,
			Protocol: "vless",
			Settings: InboundSettings{
				Clients: []Client{
					{
						ID:   cfg.UUID,
						Flow: "xtls-rprx-vision",
					},
				},
				Decryption: "none",
				Fallbacks: []Fallback{
					{Dest: 3002},
					{Dest: 3003, Path: "/vless-argo"},
					{Dest: 3004, Path: "/vmess-argo"},
					{Dest: 3005, Path: "/trojan-argo"},
				},
			},
			StreamSettings: StreamSettings{
				Network: "tcp",
			},
		},
		{
			Port:   3002,
			Listen: "127.0.0.1",
			Protocol: "vless",
			Settings: InboundSettings{
				Clients: []Client{{ID: cfg.UUID}},
				Decryption: "none",
			},
			StreamSettings: StreamSettings{
				Network:  "tcp",
				Security: "none",
			},
		},
		{
			Port:   3003,
			Listen: "127.0.0.1",
			Protocol: "vless",
			Settings: InboundSettings{
				Clients: []Client{{ID: cfg.UUID, Level: 0}},
				Decryption: "none",
			},
			StreamSettings: StreamSettings{
				Network:  "ws",
				Security: "none",
				WSSettings: &WSSettings{
					Path: "/vless-argo",
				},
			},
			Sniffing: &Sniffing{
				Enabled:      true,
				DestOverride: []string{"http", "tls", "quic"},
				MetadataOnly: false,
			},
		},
		{
			Port:   3004,
			Listen: "127.0.0.1",
			Protocol: "vmess",
			Settings: InboundSettings{
				Clients: []Client{{ID: cfg.UUID, AlterID: 0}},
			},
			StreamSettings: StreamSettings{
				Network: "ws",
				WSSettings: &WSSettings{
					Path: "/vmess-argo",
				},
			},
			Sniffing: &Sniffing{
				Enabled:      true,
				DestOverride: []string{"http", "tls", "quic"},
				MetadataOnly: false,
			},
		},
		{
			Port:   3005,
			Listen: "127.0.0.1",
			Protocol: "trojan",
			Settings: InboundSettings{
				Clients: []Client{{Password: cfg.UUID}},
			},
			StreamSettings: StreamSettings{
				Network:  "ws",
				Security: "none",
				WSSettings: &WSSettings{
					Path: "/trojan-argo",
				},
			},
			Sniffing: &Sniffing{
				Enabled:      true,
				DestOverride: []string{"http", "tls", "quic"},
				MetadataOnly: false,
			},
		},
	}

	// Outbounds配置
	config.Outbounds = []Outbound{
		{
			Protocol: "freedom",
			Tag:      "direct",
			Settings: map[string]interface{}{
				"domainStrategy": "UseIP",
			},
		},
		{
			Protocol: "blackhole",
			Tag:      "block",
			Settings: map[string]interface{}{},
		},
	}

	// Routing配置
	config.Routing.DomainStrategy = "IPIfNonMatch"
	config.Routing.Rules = []interface{}{}

	// 写入配置文件
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePaths["config"], data, 0644)
}

// 获取系统架构
func getSystemArchitecture() string {
	arch := runtime.GOARCH
	if arch == "arm" || arch == "arm64" || strings.Contains(arch, "aarch") {
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

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，状态码: %d", resp.StatusCode)
	}

	out, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	// 设置执行权限
	if err := os.Chmod(filePath, 0755); err != nil {
		return err
	}

	log.Printf("下载 %s 成功", filepath.Base(filePath))
	return nil
}

// 下载并运行依赖文件
func downloadAndRunFiles() error {
	architecture := getSystemArchitecture()
	
	// 根据架构选择下载URL
	baseURL := "https://amd64.ssss.nyc.mn/"
	if architecture == "arm" {
		baseURL = "https://arm64.ssss.nyc.mn/"
	}

	// 下载Xray
	if err := downloadFile(filePaths["web"], baseURL+"web"); err != nil {
		return fmt.Errorf("下载Xray失败: %v", err)
	}

	// 下载cloudflared
	if err := downloadFile(filePaths["bot"], baseURL+"bot"); err != nil {
		return fmt.Errorf("下载cloudflared失败: %v", err)
	}

	// 如果需要哪吒监控
	if cfg.NezhaServer != "" && cfg.NezhaKey != "" {
		if cfg.NezhaPort != "" {
			// 哪吒v0
			if err := downloadFile(filePaths["npm"], baseURL+"agent"); err != nil {
				return fmt.Errorf("下载哪吒agent失败: %v", err)
			}
		} else {
			// 哪吒v1
			if err := downloadFile(filePaths["php"], baseURL+"v1"); err != nil {
				return fmt.Errorf("下载哪吒v1失败: %v", err)
			}
		}
	}

	// 运行哪吒监控
	if cfg.NezhaServer != "" && cfg.NezhaKey != "" {
		if cfg.NezhaPort != "" {
			// 哪吒v0
			args := []string{
				"-s", cfg.NezhaServer + ":" + cfg.NezhaPort,
				"-p", cfg.NezhaKey,
				"--disable-auto-update",
				"--report-delay", "4",
				"--skip-conn",
				"--skip-procs",
			}

			// 检查是否需要TLS
			tlsPorts := map[string]bool{"443": true, "8443": true, "2096": true, "2087": true, "2083": true, "2053": true}
			if tlsPorts[cfg.NezhaPort] {
				args = append(args, "--tls")
			}

			cmd := exec.Command(filePaths["npm"], args...)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				log.Printf("启动哪吒监控失败: %v", err)
			} else {
				processes = append(processes, cmd.Process)
				log.Println("哪吒监控运行中")
			}
		} else {
			// 哪吒v1 - 生成配置文件
			port := ""
			if idx := strings.LastIndex(cfg.NezhaServer, ":"); idx != -1 {
				port = cfg.NezhaServer[idx+1:]
			}

			tlsPorts := map[string]bool{"443": true, "8443": true, "2096": true, "2087": true, "2083": true, "2053": true}
			nezhaTLS := "false"
			if tlsPorts[port] {
				nezhaTLS = "true"
			}

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
uuid: %s`, cfg.NezhaKey, cfg.NezhaServer, nezhaTLS, cfg.UUID)

			if err := os.WriteFile(filePaths["nezhaConfig"], []byte(configYaml), 0644); err != nil {
				log.Printf("写入哪吒配置文件失败: %v", err)
			}

			cmd := exec.Command(filePaths["php"], "-c", filePaths["nezhaConfig"])
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				log.Printf("启动哪吒v1失败: %v", err)
			} else {
				processes = append(processes, cmd.Process)
				log.Println("哪吒v1运行中")
			}
		}
		time.Sleep(1 * time.Second)
	}

	// 运行Xray
	cmd := exec.Command(filePaths["web"], "-c", filePaths["config"])
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动Xray失败: %v", err)
	}
	processes = append(processes, cmd.Process)
	log.Println("Xray运行中")
	time.Sleep(1 * time.Second)

	// 运行cloudflared
	return runCloudflared()
}

// 运行cloudflared隧道
func runCloudflared() error {
	args := []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate", "--protocol", "http2"}

	if cfg.ArgoAuth != "" && len(cfg.ArgoAuth) >= 120 && len(cfg.ArgoAuth) <= 250 && 
	   strings.Contains(cfg.ArgoAuth, "=") {
		// Token模式
		args = append(args, "run", "--token", cfg.ArgoAuth)
	} else if cfg.ArgoAuth != "" && strings.Contains(cfg.ArgoAuth, "TunnelSecret") {
		// TunnelSecret模式
		if err := generateTunnelConfig(); err != nil {
			return err
		}
		args = append(args, "--config", filePaths["tunnelYaml"], "run")
	} else {
		// 快速隧道模式
		args = append(args, "--logfile", filePaths["bootLog"], "--loglevel", "info",
			"--url", fmt.Sprintf("http://localhost:%s", cfg.ArgoPort))
	}

	cmd := exec.Command(filePaths["bot"], args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动cloudflared失败: %v", err)
	}
	processes = append(processes, cmd.Process)
	log.Println("cloudflared运行中")

	// 等待隧道启动
	time.Sleep(5 * time.Second)
	return nil
}

// 生成隧道配置文件
func generateTunnelConfig() error {
	// 写入JSON配置文件
	if err := os.WriteFile(filePaths["tunnelJson"], []byte(cfg.ArgoAuth), 0644); err != nil {
		return err
	}

	// 解析TunnelID
	var tunnelData map[string]interface{}
	if err := json.Unmarshal([]byte(cfg.ArgoAuth), &tunnelData); err != nil {
		return err
	}

	tunnelID, ok := tunnelData["TunnelID"].(string)
	if !ok {
		return fmt.Errorf("无法解析TunnelID")
	}

	// 生成YAML配置
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
`, tunnelID, filePaths["tunnelJson"], cfg.ArgoDomain, cfg.ArgoPort)

	return os.WriteFile(filePaths["tunnelYaml"], []byte(tunnelYaml), 0644)
}

// 获取ISP信息
func getMetaInfo() string {
	type IPAPIResponse struct {
		CountryCode string `json:"country_code"`
		Org         string `json:"org"`
	}

	// 尝试第一个API
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://ipapi.co/json/")
	if err == nil {
		defer resp.Body.Close()
		var data IPAPIResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			if data.CountryCode != "" && data.Org != "" {
				return fmt.Sprintf("%s_%s", data.CountryCode, data.Org)
			}
		}
	}

	// 尝试第二个API
	type IPAPIComResponse struct {
		Status      string `json:"status"`
		CountryCode string `json:"countryCode"`
		Org         string `json:"org"`
	}

	resp, err = client.Get("http://ip-api.com/json/")
	if err == nil {
		defer resp.Body.Close()
		var data IPAPIComResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			if data.Status == "success" && data.CountryCode != "" && data.Org != "" {
				return fmt.Sprintf("%s_%s", data.CountryCode, data.Org)
			}
		}
	}

	return "Unknown"
}

// 生成订阅内容
func generateSubContent(argoDomain string) string {
	isp := getMetaInfo()
	nodeName := isp
	if cfg.Name != "" {
		nodeName = cfg.Name + "-" + isp
	}

	// VMESS配置
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

	return fmt.Sprintf(`vless://%s@%s:%s?encryption=none&security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Fvless-argo%%3Fed%%3D2560#%s

vmess://%s

trojan://%s@%s:%s?security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Ftrojan-argo%%3Fed%%3D2560#%s`,
		cfg.UUID, cfg.CFIP, cfg.CFPort, argoDomain, argoDomain, nodeName,
		vmessBase64,
		cfg.UUID, cfg.CFIP, cfg.CFPort, argoDomain, argoDomain, nodeName)
}

// 上传节点
func uploadNodes(subContent string) {
	if cfg.UploadURL == "" {
		return
	}

	subBase64 := base64.StdEncoding.EncodeToString([]byte(subContent))

	if cfg.ProjectURL != "" {
		// 上传订阅
		subscriptionURL := fmt.Sprintf("%s/%s", cfg.ProjectURL, cfg.SubPath)
		data := map[string]interface{}{
			"subscription": []string{subscriptionURL},
		}

		jsonData, _ := json.Marshal(data)
		resp, err := http.Post(cfg.UploadURL+"/api/add-subscriptions", 
			"application/json", bytes.NewBuffer(jsonData))
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				log.Println("订阅上传成功")
			} else if resp.StatusCode == 400 {
				log.Println("订阅已存在")
			}
		}
	} else {
		// 上传节点
		data := map[string]interface{}{
			"nodes": strings.Split(subContent, "\n"),
		}

		jsonData, _ := json.Marshal(data)
		resp, err := http.Post(cfg.UploadURL+"/api/add-nodes",
			"application/json", bytes.NewBuffer(jsonData))
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				log.Println("节点上传成功")
			}
		}
	}

	// 保存订阅文件
	os.WriteFile(filePaths["sub"], []byte(subBase64), 0644)
	log.Println("订阅文件保存成功")
}

// 自动访问项目URL
func addVisitTask() {
	if !cfg.AutoAccess || cfg.ProjectURL == "" {
		return
	}

	data := map[string]string{"url": cfg.ProjectURL}
	jsonData, _ := json.Marshal(data)

	resp, err := http.Post("https://oooo.serv00.net/add-url",
		"application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("添加自动访问任务失败: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		log.Println("自动访问任务添加成功")
	}
}

// 主启动函数
func startServer() {
	log.Println("开始服务器初始化...")

	cleanupOldFiles()

	if err := generateXrayConfig(); err != nil {
		log.Fatal("生成Xray配置失败:", err)
	}

	if err := downloadAndRunFiles(); err != nil {
		log.Fatal("下载运行文件失败:", err)
	}

	log.Println("等待隧道启动...")
	time.Sleep(5 * time.Second)

	// 生成订阅
	argoDomain := cfg.ArgoDomain
	if argoDomain == "" {
		// 尝试从日志读取临时域名
		if data, err := os.ReadFile(filePaths["bootLog"]); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.Contains(line, "trycloudflare.com") {
					if idx := strings.Index(line, "https://"); idx != -1 {
						substr := line[idx:]
						if endIdx := strings.Index(substr, " "); endIdx != -1 {
							argoDomain = substr[8:strings.Index(substr[8:], "/")+8]
							break
						}
					}
				}
			}
		}
	}

	if argoDomain == "" {
		log.Println("无法获取隧道域名，使用默认域名")
		argoDomain = "date.goyo123.ggff.net"
	}

	subContent := generateSubContent(argoDomain)
	log.Println("生成的订阅内容:")
	fmt.Println(base64.StdEncoding.EncodeToString([]byte(subContent)))

	uploadNodes(subContent)
	addVisitTask()

	log.Println("服务器初始化完成")
}

// 代理处理器
func proxyHandler(c *gin.Context) {
	urlPath := c.Request.URL.Path

	if strings.HasPrefix(urlPath, "/vless-argo") ||
		strings.HasPrefix(urlPath, "/vmess-argo") ||
		strings.HasPrefix(urlPath, "/trojan-argo") ||
		urlPath == "/vless" ||
		urlPath == "/vmess" ||
		urlPath == "/trojan" {
		// 转发到Xray
		proxy := httputil.NewSingleHostReverseProxy(&url.URL{
			Scheme: "http",
			Host:   "localhost:3001",
		})
		proxy.ServeHTTP(c.Writer, c.Request)
	} else {
		c.Next()
	}
}

// WebSocket代理
func wsProxyHandler(c *gin.Context) {
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("WebSocket升级失败:", err)
		return
	}
	defer conn.Close()

	// 连接到后端WebSocket服务
	targetURL := "ws://localhost:3001" + c.Request.URL.Path
	targetConn, _, err := websocket.DefaultDialer.Dial(targetURL, nil)
	if err != nil {
		log.Println("连接到后端WebSocket失败:", err)
		return
	}
	defer targetConn.Close()

	// 双向转发
	done := make(chan struct{})
	
	go func() {
		defer close(done)
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := targetConn.WriteMessage(messageType, message); err != nil {
				return
			}
		}
	}()

	go func() {
		defer close(done)
		for {
			messageType, message, err := targetConn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(messageType, message); err != nil {
				return
			}
		}
	}()

	<-done
}

// 清理文件
func cleanFiles() {
	time.Sleep(90 * time.Second)

	filesToDelete := []string{
		filePaths["bootLog"],
		filePaths["config"],
		filePaths["web"],
		filePaths["bot"],
		filePaths["monitor"],
	}

	if cfg.NezhaPort != "" {
		filesToDelete = append(filesToDelete, filePaths["npm"])
	} else if cfg.NezhaServer != "" && cfg.NezhaKey != "" {
		filesToDelete = append(filesToDelete, filePaths["php"])
	}

	for _, file := range filesToDelete {
		if err := os.Remove(file); err == nil {
			log.Printf("已删除文件: %s", file)
		}
	}

	log.Println("应用正在运行")
	log.Println("感谢使用此脚本，享受吧！")
}

func main() {
	initConfig()

	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 设置路由
	r.Any("/*path", func(c *gin.Context) {
		urlPath := c.Param("path")
		
		if strings.HasPrefix(urlPath, "/"+cfg.SubPath) {
			// 订阅路由
			argoDomain := cfg.ArgoDomain
			if argoDomain == "" {
				argoDomain = "date.goyo123.ggff.net"
			}
			subContent := generateSubContent(argoDomain)
			encoded := base64.StdEncoding.EncodeToString([]byte(subContent))
			c.String(200, encoded)
			return
		}

		// 代理处理
		proxyHandler(c)
	})

	// 启动HTTP服务器
	go func() {
		log.Printf("HTTP服务运行在端口: %s", cfg.Port)
		if err := r.Run(":" + cfg.Port); err != nil {
			log.Fatal("HTTP服务启动失败:", err)
		}
	}()

	// 启动代理服务器
	go func() {
		proxyServer := &http.Server{
			Addr: ":" + cfg.ArgoPort,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if websocket.IsWebSocketUpgrade(r) {
					wsProxyHandler(gin.WrapF(func(w http.ResponseWriter, r *http.Request) {
						// WebSocket处理
						wsProxyHandler(&gin.Context{Request: r, Writer: w})
					})(w, r))
					return
				}

				// HTTP代理
				urlPath := r.URL.Path
				target := "http://localhost:" + cfg.Port

				if strings.HasPrefix(urlPath, "/vless-argo") ||
					strings.HasPrefix(urlPath, "/vmess-argo") ||
					strings.HasPrefix(urlPath, "/trojan-argo") ||
					urlPath == "/vless" ||
					urlPath == "/vmess" ||
					urlPath == "/trojan" {
					target = "http://localhost:3001"
				}

				proxy := httputil.NewSingleHostReverseProxy(&url.URL{
					Scheme: "http",
					Host:   target[7:], // 去掉"http://"
				})
				proxy.ServeHTTP(w, r)
			}),
		}

		log.Printf("代理服务器启动在端口: %s", cfg.ArgoPort)
		log.Printf("HTTP流量 -> localhost:%s", cfg.Port)
		log.Printf("Xray流量 -> localhost:3001")
		
		if err := proxyServer.ListenAndServe(); err != nil {
			log.Fatal("代理服务器启动失败:", err)
		}
	}()

	// 启动主服务
	go startServer()

	// 清理文件
	go cleanFiles()

	// 信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Println("收到关闭信号，正在清理...")

	// 停止所有子进程
	for _, p := range processes {
		if p != nil {
			p.Kill()
		}
	}

	log.Println("程序退出")
}
