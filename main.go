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
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ---------------------------- 日志级别 ----------------------------
var (
	logLevel = getLogLevel()
)

type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

func getLogLevel() LogLevel {
	levelStr := strings.ToLower(os.Getenv("LOG_LEVEL"))
	switch levelStr {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func logDebug(format string, v ...interface{}) {
	if logLevel <= LevelDebug {
		log.Printf("[DEBUG] "+format, v...)
	}
}
func logInfo(format string, v ...interface{}) {
	if logLevel <= LevelInfo {
		log.Printf("[INFO] "+format, v...)
	}
}
func logWarn(format string, v ...interface{}) {
	if logLevel <= LevelWarn {
		log.Printf("[WARN] "+format, v...)
	}
}
func logError(format string, v ...interface{}) {
	if logLevel <= LevelError {
		log.Printf("[ERROR] "+format, v...)
	}
}

// ---------------------------- 配置结构体 ----------------------------
type Config struct {
	UploadURL     string
	ProjectURL    string
	AutoAccess    bool
	FilePath      string
	SubPath       string
	Port          string
	ArgoPort      string
	UUID          string
	NezhaServer   string
	NezhaPort     string
	NezhaKey      string
	ArgoDomain    string
	ArgoAuth      string
	CFIP          string
	CFPort        string
	Name          string
	MonitorKey    string
	MonitorServer string
	MonitorURL    string
}

// ---------------------------- 全局变量 ----------------------------
var (
	config       Config
	appFiles     *AppFiles
	subscription string
	mu           sync.RWMutex

	// HTTP 客户端复用
	httpClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// 统计信息
	wsConnections int64
	totalBytes    int64

	// 进程管理器
	procMgr = NewProcessManager()
)

// ---------------------------- 文件路径管理器 ----------------------------
type AppFiles struct {
	dir          string
	randomPrefix string
}

func NewAppFiles(baseDir string) *AppFiles {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		logError("生成随机前缀失败: %v", err)
		// fallback: 使用时间戳并补零至6位
		ts := fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
		b = []byte(ts)
	}
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	prefix := string(b)

	return &AppFiles{
		dir:          baseDir,
		randomPrefix: prefix,
	}
}

func (f *AppFiles) path(name string) string {
	return filepath.Join(f.dir, f.randomPrefix+"-"+name)
}

func (f *AppFiles) Npm() string       { return f.path("npm") }
func (f *AppFiles) Web() string        { return f.path("web") }
func (f *AppFiles) Bot() string        { return f.path("bot") }
func (f *AppFiles) Php() string        { return f.path("php") }
func (f *AppFiles) Monitor() string    { return filepath.Join(f.dir, "cf-vps-monitor.sh") }
func (f *AppFiles) Sub() string        { return f.path("sub.txt") }
func (f *AppFiles) List() string       { return f.path("list.txt") }
func (f *AppFiles) BootLog() string    { return f.path("boot.log") }
func (f *AppFiles) Config() string     { return f.path("config.json") }
func (f *AppFiles) NezhaConfig() string { return f.path("config.yaml") }
func (f *AppFiles) TunnelJson() string { return f.path("tunnel.json") }
func (f *AppFiles) TunnelYaml() string { return f.path("tunnel.yml") }

func (f *AppFiles) AllTempFiles() []string {
	return []string{
		f.Npm(),
		f.Web(),
		f.Bot(),
		f.Php(),
		f.Monitor(),
		f.Sub(),
		f.List(),
		f.BootLog(),
		f.Config(),
		f.NezhaConfig(),
		f.TunnelJson(),
		f.TunnelYaml(),
	}
}

// ---------------------------- 进程管理器 (修复版) ----------------------------
type ManagedProcess struct {
	Cmd      *exec.Cmd
	Name     string
	BinPath  string          // 保存可执行文件路径，用于重启
	Args     []string        // 保存启动参数，用于重启
	Stop     context.CancelFunc
	Done     chan error      // 用于通知 Stop 进程已退出
	Restart  bool
	ExitCode int
}

type ProcessManager struct {
	mu       sync.Mutex
	procs    map[string]*ManagedProcess
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewProcessManager() *ProcessManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &ProcessManager{
		procs:  make(map[string]*ManagedProcess),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start 启动进程并记录参数，以便重启
func (pm *ProcessManager) Start(name, binPath string, args []string, restart bool) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.procs[name]; exists {
		return fmt.Errorf("进程 %s 已存在", name)
	}

	ctx, stop := context.WithCancel(pm.ctx)
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		stop()
		return err
	}

	proc := &ManagedProcess{
		Cmd:      cmd,
		Name:     name,
		BinPath:  binPath,
		Args:     args,
		Stop:     stop,
		Done:     make(chan error, 1),
		Restart:  restart,
	}
	pm.procs[name] = proc

	pm.wg.Add(1)
	go pm.waitProcess(proc)

	logInfo("进程 %s (PID %d) 已启动", name, cmd.Process.Pid)
	return nil
}

// waitProcess 等待进程退出，处理重启，并通知 Done 通道
func (pm *ProcessManager) waitProcess(p *ManagedProcess) {
	defer pm.wg.Done()
	err := p.Cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		logWarn("进程 %s 退出，错误: %v", p.Name, err)
	} else {
		logInfo("进程 %s 正常退出", p.Name)
	}
	p.ExitCode = exitCode

	// 从管理器中移除
	pm.mu.Lock()
	delete(pm.procs, p.Name)
	pm.mu.Unlock()

	// 通知 Stop 调用者进程已退出
	p.Done <- err

	// 如果需要重启且程序尚未被取消
	if p.Restart && pm.ctx.Err() == nil {
		logInfo("进程 %s 将在5秒后重启", p.Name)
		time.Sleep(5 * time.Second)
		// 重新启动（注意：重启时不能再用原来的 context，需新建）
		// 这里简化处理：重新调用 Start，但需注意 Start 内部会加锁检查是否存在同名进程
		// 由于我们已经从 procs 中删除，所以可以重新启动
		newCtx, newStop := context.WithCancel(pm.ctx)
		newCmd := exec.CommandContext(newCtx, p.BinPath, p.Args...)
		newCmd.Stdout = nil
		newCmd.Stderr = nil
		if err := newCmd.Start(); err != nil {
			logError("重启进程 %s 失败: %v", p.Name, err)
			newStop()
			return
		}
		newProc := &ManagedProcess{
			Cmd:      newCmd,
			Name:     p.Name,
			BinPath:  p.BinPath,
			Args:     p.Args,
			Stop:     newStop,
			Done:     make(chan error, 1),
			Restart:  p.Restart,
		}
		pm.mu.Lock()
		pm.procs[p.Name] = newProc
		pm.mu.Unlock()
		pm.wg.Add(1)
		go pm.waitProcess(newProc)
		logInfo("进程 %s (PID %d) 重启成功", p.Name, newCmd.Process.Pid)
	}
}

// Stop 停止指定名称的进程
func (pm *ProcessManager) Stop(name string) error {
	pm.mu.Lock()
	p, ok := pm.procs[name]
	pm.mu.Unlock()
	if !ok {
		return nil
	}
	p.Stop() // 发送 SIGTERM (通过 context)
	// 等待进程退出或超时
	select {
	case <-p.Done:
		// 进程已退出
	case <-time.After(10 * time.Second):
		// 强制杀死
		if p.Cmd.Process != nil {
			p.Cmd.Process.Kill()
		}
	}
	return nil
}

// StopAll 停止所有进程
func (pm *ProcessManager) StopAll() {
	pm.cancel() // 取消全局 context，所有子进程都会收到信号
	pm.wg.Wait()
}

// ---------------------------- 主函数 ----------------------------
func main() {
	initConfig()
	tunePerformance()

	if err := os.MkdirAll(config.FilePath, 0755); err != nil {
		logError("创建目录失败: %v", err)
	} else {
		logInfo("目录 %s 已创建或已存在", config.FilePath)
	}

	appFiles = NewAppFiles(config.FilePath)
	cleanupOld()
	generateXrayConfig()
	argoType()

	go startProxyServer()
	go startHTTPServer()
	go startMainProcess()

	setupSignalHandler()
	select {}
}

func initConfig() {
	config = Config{
		UploadURL:     getEnv("UPLOAD_URL", ""),
		ProjectURL:    getEnv("PROJECT_URL", ""),
		AutoAccess:    getEnv("AUTO_ACCESS", "false") == "true",
		FilePath:      getEnv("FILE_PATH", "./tmp"),
		SubPath:       getEnv("SUB_PATH", "sub"),
		Port:          getEnv("SERVER_PORT", getEnv("PORT", "3000")),
		ArgoPort:      getEnv("ARGO_PORT", "7860"),
		UUID:          getEnv("UUID", "e2cae6af-5cdd-fa48-4137-ad3e617fbab0"),
		NezhaServer:   getEnv("NEZHA_SERVER", ""),
		NezhaPort:     getEnv("NEZHA_PORT", ""),
		NezhaKey:      getEnv("NEZHA_KEY", ""),
		ArgoDomain:    getEnv("ARGO_DOMAIN", ""),
		ArgoAuth:      getEnv("ARGO_AUTH", ""),
		CFIP:          getEnv("CFIP", "cdns.doon.eu.org"),
		CFPort:        getEnv("CFPORT", "443"),
		Name:          getEnv("NAME", ""),
		MonitorKey:    getEnv("MONITOR_KEY", ""),
		MonitorServer: getEnv("MONITOR_SERVER", ""),
		MonitorURL:    getEnv("MONITOR_URL", ""),
	}

	logInfo("配置初始化完成")
	logInfo("UUID: %s", config.UUID)
	logInfo("Argo端口: %s", config.ArgoPort)
	logInfo("HTTP端口: %s", config.Port)
}

func tunePerformance() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	logDebug("性能调优: GOMAXPROCS=%d, CPU核心数=%d", runtime.GOMAXPROCS(0), runtime.NumCPU())
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func cleanupOld() {
	deleteNodes()
	if err := os.RemoveAll(config.FilePath); err != nil {
		logWarn("清理目录失败: %v", err)
	}
	os.MkdirAll(config.FilePath, 0755)
}

func deleteNodes() {
	if config.UploadURL == "" {
		return
	}
	data, err := os.ReadFile(appFiles.Sub())
	if err != nil {
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return
	}
	lines := strings.Split(string(decoded), "\n")
	var nodes []string
	for _, line := range lines {
		if isProxyLink(line) {
			nodes = append(nodes, line)
		}
	}
	if len(nodes) == 0 {
		return
	}
	jsonData, _ := json.Marshal(map[string][]string{"nodes": nodes})
	req, _ := http.NewRequest("POST", config.UploadURL+"/api/delete-nodes", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	_, err = httpClient.Do(req)
	if err != nil {
		logWarn("删除节点失败: %v", err)
	}
}

func isProxyLink(line string) bool {
	return strings.Contains(line, "vless://") ||
		strings.Contains(line, "vmess://") ||
		strings.Contains(line, "trojan://") ||
		strings.Contains(line, "hysteria2://") ||
		strings.Contains(line, "tuic://")
}

// ---------------------------- 配置生成 ----------------------------
func generateXrayConfig() {
	xrayConfig := map[string]interface{}{
		"log": map[string]interface{}{
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
		"inbounds":  generateInbounds(),
		"outbounds": generateOutbounds(),
		"routing": map[string]interface{}{
			"domainStrategy": "IPIfNonMatch",
			"rules":          []interface{}{},
		},
	}

	data, err := json.MarshalIndent(xrayConfig, "", "  ")
	if err != nil {
		logError("生成配置文件失败: %v", err)
		return
	}
	if err := os.WriteFile(appFiles.Config(), data, 0644); err != nil {
		logError("写入配置文件失败: %v", err)
		return
	}
	logInfo("Xray配置文件生成完成")
}

func generateInbounds() []map[string]interface{} {
	uuid := config.UUID
	return []map[string]interface{}{
		{
			"port":     3001,
			"protocol": "vless",
			"settings": map[string]interface{}{
				"clients": []map[string]interface{}{
					{"id": uuid, "flow": "xtls-rprx-vision"},
				},
				"decryption": "none",
				"fallbacks": []map[string]interface{}{
					{"dest": 3002},
					{"path": "/vless-argo", "dest": 3003},
					{"path": "/vmess-argo", "dest": 3004},
					{"path": "/trojan-argo", "dest": 3005},
				},
			},
			"streamSettings": map[string]interface{}{"network": "tcp"},
		},
		{
			"port":     3002,
			"listen":   "127.0.0.1",
			"protocol": "vless",
			"settings": map[string]interface{}{
				"clients":    []map[string]interface{}{{"id": uuid}},
				"decryption": "none",
			},
			"streamSettings": map[string]interface{}{"network": "tcp", "security": "none"},
		},
		{
			"port":     3003,
			"listen":   "127.0.0.1",
			"protocol": "vless",
			"settings": map[string]interface{}{
				"clients":    []map[string]interface{}{{"id": uuid, "level": 0}},
				"decryption": "none",
			},
			"streamSettings": map[string]interface{}{
				"network":  "ws",
				"security": "none",
				"wsSettings": map[string]interface{}{"path": "/vless-argo"},
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
				"clients": []map[string]interface{}{{"id": uuid, "alterId": 0}},
			},
			"streamSettings": map[string]interface{}{
				"network": "ws",
				"wsSettings": map[string]interface{}{"path": "/vmess-argo"},
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
				"clients": []map[string]interface{}{{"password": uuid}},
			},
			"streamSettings": map[string]interface{}{
				"network":  "ws",
				"security": "none",
				"wsSettings": map[string]interface{}{"path": "/trojan-argo"},
			},
			"sniffing": map[string]interface{}{
				"enabled":      true,
				"destOverride": []string{"http", "tls", "quic"},
				"metadataOnly": false,
			},
		},
	}
}

func generateOutbounds() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"protocol": "freedom",
			"tag":      "direct",
			"settings": map[string]interface{}{"domainStrategy": "UseIP"},
		},
		{
			"protocol": "blackhole",
			"tag":      "block",
			"settings": map[string]interface{}{},
		},
	}
}

func argoType() {
	if config.ArgoAuth == "" || config.ArgoDomain == "" {
		logInfo("ARGO_DOMAIN 或 ARGO_AUTH 为空，使用快速隧道")
		return
	}
	if strings.Contains(config.ArgoAuth, "TunnelSecret") {
		var tunnelConfig map[string]interface{}
		if err := json.Unmarshal([]byte(config.ArgoAuth), &tunnelConfig); err != nil {
			logError("解析隧道配置失败: %v", err)
			return
		}
		if err := os.WriteFile(appFiles.TunnelJson(), []byte(config.ArgoAuth), 0644); err != nil {
			logError("写入tunnel.json失败: %v", err)
			return
		}
		tunnelID, _ := tunnelConfig["TunnelID"].(string)
		yamlContent := fmt.Sprintf(`tunnel: %s
credentials-file: %s
protocol: http2

ingress:
  - hostname: %s
    service: http://localhost:%s
    originRequest:
      noTLSVerify: true
  - service: http_status:404
`, tunnelID, appFiles.TunnelJson(), config.ArgoDomain, config.ArgoPort)

		if err := os.WriteFile(appFiles.TunnelYaml(), []byte(yamlContent), 0644); err != nil {
			logError("写入tunnel.yml失败: %v", err)
			return
		}
		logInfo("隧道YAML配置生成成功")
	} else {
		logInfo("ARGO_AUTH 不是TunnelSecret格式，使用token连接隧道")
	}
}

// ---------------------------- 代理服务器 ----------------------------
func startProxyServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/stats", handleStats)
	mux.HandleFunc("/", handleProxyRequest)

	proxyServer := &http.Server{
		Addr:         ":" + config.ArgoPort,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	logInfo("代理服务器启动在端口: %s", config.ArgoPort)

	if err := proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logError("代理服务器启动失败: %v", err)
	}
}

func handleProxyRequest(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path
	isWebSocket := strings.ToLower(r.Header.Get("Upgrade")) == "websocket"
	targetHost := "localhost"
	var targetPort string

	if strings.HasPrefix(urlPath, "/vless-argo") ||
		strings.HasPrefix(urlPath, "/vmess-argo") ||
		strings.HasPrefix(urlPath, "/trojan-argo") ||
		urlPath == "/vless" || urlPath == "/vmess" || urlPath == "/trojan" {
		targetPort = "3001"
	} else {
		targetPort = config.Port
	}

	if isWebSocket {
		handleWebSocketProxy(w, r, targetHost, targetPort)
	} else {
		handleHTTPProxy(w, r, targetHost, targetPort)
	}
}

func handleWebSocketProxy(w http.ResponseWriter, r *http.Request, targetHost, targetPort string) {
	if r.Method != "GET" || strings.ToLower(r.Header.Get("Upgrade")) != "websocket" ||
		strings.ToLower(r.Header.Get("Connection")) != "upgrade" {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	backendAddr := net.JoinHostPort(targetHost, targetPort)
	backendConn, err := net.DialTimeout("tcp", backendAddr, 10*time.Second)
	if err != nil {
		logError("连接后端失败: %v", err)
		http.Error(w, "Backend connection failed", http.StatusBadGateway)
		return
	}
	defer backendConn.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		logError("劫持连接失败: %v", err)
		http.Error(w, "Hijack failed", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	if clientBuf != nil && clientBuf.Reader.Buffered() > 0 {
		buffered, _ := io.ReadAll(clientBuf.Reader)
		if _, err := backendConn.Write(buffered); err != nil {
			return
		}
	}
	if err := r.Write(backendConn); err != nil {
		return
	}

	atomic.AddInt64(&wsConnections, 1)
	defer atomic.AddInt64(&wsConnections, -1)

	tcpConn, ok := clientConn.(*net.TCPConn)
	if ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(60 * time.Second)
	}
	tcpBackend, ok := backendConn.(*net.TCPConn)
	if ok {
		tcpBackend.SetKeepAlive(true)
		tcpBackend.SetKeepAlivePeriod(60 * time.Second)
	}

	buf := make([]byte, 64*1024)
	errCh := make(chan error, 2)

	go func() {
		_, err := io.CopyBuffer(backendConn, clientConn, buf)
		errCh <- err
	}()
	go func() {
		_, err := io.CopyBuffer(clientConn, backendConn, buf)
		errCh <- err
	}()

	// 等待任意一端出错或完成
	<-errCh
	// 连接关闭后另一个 goroutine 会自然退出
}

var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 32*1024)
	},
}

func handleHTTPProxy(w http.ResponseWriter, r *http.Request, targetHost, targetPort string) {
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = fmt.Sprintf("%s:%s", targetHost, targetPort)
			req.Host = req.URL.Host
			if _, ok := req.Header["User-Agent"]; !ok {
				req.Header.Set("User-Agent", "Argo-Tunnel-Proxy/1.0")
			}
		},
		BufferPool: bufferPool,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logError("HTTP代理错误: %v", err)
			http.Error(w, "Proxy error", http.StatusInternalServerError)
		},
	}
	proxy.ServeHTTP(w, r)
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	procMgr.mu.Lock()
	procs := make([]map[string]interface{}, 0, len(procMgr.procs))
	for name, p := range procMgr.procs {
		procs = append(procs, map[string]interface{}{
			"name":     name,
			"pid":      p.Cmd.Process.Pid,
			"running":  true,
			"restart":  p.Restart,
		})
	}
	procMgr.mu.Unlock()

	stats := map[string]interface{}{
		"ws_connections": atomic.LoadInt64(&wsConnections),
		"total_bytes":    atomic.LoadInt64(&totalBytes),
		"goroutines":     runtime.NumGoroutine(),
		"memory": map[string]interface{}{
			"alloc":       formatBytes(int64(memStats.Alloc)),
			"total_alloc": formatBytes(int64(memStats.TotalAlloc)),
			"sys":         formatBytes(int64(memStats.Sys)),
			"num_gc":      memStats.NumGC,
		},
		"processes": procs,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// ---------------------------- HTTP服务器 ----------------------------
func startHTTPServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/"+config.SubPath, func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		encoded := base64.StdEncoding.EncodeToString([]byte(subscription))
		mu.RUnlock()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(encoded))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		indexPaths := []string{"index.html", "/app/index.html", "./index.html"}
		for _, indexPath := range indexPaths {
			if _, err := os.Stat(indexPath); err == nil {
				http.ServeFile(w, r, indexPath)
				return
			}
		}
		w.Write([]byte("Hello world!"))
	})

	server := &http.Server{
		Addr:         ":" + config.Port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	logInfo("HTTP服务运行在内部端口: %s", config.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logError("HTTP服务器启动失败: %v", err)
	}
}

// ---------------------------- 主流程 ----------------------------
func startMainProcess() {
	logInfo("开始服务器初始化...")
	downloadFilesAndRun()
	logInfo("等待隧道启动...")
	time.Sleep(5 * time.Second)
	extractDomains()
	addVisitTask()
	go startMonitorScript()
	logInfo("服务器初始化完成")
}

func downloadFilesAndRun() {
	arch := getArchitecture()
	baseURL := "https://amd64.ssss.nyc.mn/"
	if arch == "arm" {
		baseURL = "https://arm64.ssss.nyc.mn/"
	}

	type downloadItem struct {
		name string
		path string
		url  string
	}
	var downloads []downloadItem

	downloads = append(downloads,
		downloadItem{"web", appFiles.Web(), baseURL + "web"},
		downloadItem{"bot", appFiles.Bot(), baseURL + "bot"},
	)

	if config.NezhaServer != "" && config.NezhaKey != "" {
		if config.NezhaPort != "" {
			downloads = append(downloads, downloadItem{"agent", appFiles.Npm(), baseURL + "agent"})
		} else {
			downloads = append(downloads, downloadItem{"v1", appFiles.Php(), baseURL + "v1"})
		}
	}

	downloadWithLimit(downloads, 3)
	runNezha()
	runXray()
	runCloudflared()
}

func downloadWithLimit(items []downloadItem, limit int) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, limit)

	for _, item := range items {
		wg.Add(1)
		go func(item downloadItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := downloadFile(item.path, item.url); err != nil {
				logError("下载 %s 失败: %v", item.name, err)
			} else {
				logInfo("下载 %s 成功", item.name)
				os.Chmod(item.path, 0755)
			}
		}(item)
	}
	wg.Wait()
}

func getArchitecture() string {
	arch := runtime.GOARCH
	if arch == "arm" || arch == "arm64" || arch == "aarch64" {
		return "arm"
	}
	return "amd"
}

func downloadFile(filepath, url string) error {
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: %s", resp.Status)
	}
	_, err = io.Copy(out, resp.Body)
	return err
}

func runNezha() {
	if config.NezhaServer == "" || config.NezhaKey == "" {
		logInfo("哪吒监控变量为空，跳过运行")
		return
	}
	if config.NezhaPort == "" {
		port := "443"
		if idx := strings.LastIndex(config.NezhaServer, ":"); idx != -1 {
			port = config.NezhaServer[idx+1:]
		}
		tlsPorts := map[string]bool{"443": true, "8443": true, "2096": true, "2087": true, "2083": true, "2053": true}
		nezhatls := "false"
		if tlsPorts[port] {
			nezhatls = "true"
		}
		yamlContent := fmt.Sprintf(`client_secret: %s
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
uuid: %s`, config.NezhaKey, config.NezhaServer, nezhatls, config.UUID)

		if err := os.WriteFile(appFiles.NezhaConfig(), []byte(yamlContent), 0644); err != nil {
			logError("生成哪吒配置失败: %v", err)
			return
		}
		if err := procMgr.Start("nezha", appFiles.Php(), []string{"-c", appFiles.NezhaConfig()}, false); err != nil {
			logError("运行哪吒失败: %v", err)
		}
		time.Sleep(1 * time.Second)
	} else {
		args := []string{"-s", config.NezhaServer + ":" + config.NezhaPort, "-p", config.NezhaKey}
		tlsPorts := map[string]bool{"443": true, "8443": true, "2096": true, "2087": true, "2083": true, "2053": true}
		if tlsPorts[config.NezhaPort] {
			args = append(args, "--tls")
		}
		args = append(args, "--disable-auto-update", "--report-delay", "4", "--skip-conn", "--skip-procs")
		if err := procMgr.Start("nezha", appFiles.Npm(), args, false); err != nil {
			logError("运行哪吒失败: %v", err)
		}
		time.Sleep(1 * time.Second)
	}
}

func runXray() {
	if err := procMgr.Start("xray", appFiles.Web(), []string{"-c", appFiles.Config()}, false); err != nil {
		logError("运行Xray失败: %v", err)
	} else {
		time.Sleep(1 * time.Second)
	}
}

func runCloudflared() {
	if _, err := os.Stat(appFiles.Bot()); err != nil {
		logError("cloudflared文件不存在")
		return
	}
	args := []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate", "--protocol", "http2"}
	if config.ArgoAuth != "" && config.ArgoDomain != "" {
		if len(config.ArgoAuth) >= 120 && len(config.ArgoAuth) <= 250 {
			args = append(args, "run", "--token", config.ArgoAuth)
		} else if strings.Contains(config.ArgoAuth, "TunnelSecret") {
			for i := 0; i < 10; i++ {
				if _, err := os.Stat(appFiles.TunnelYaml()); err == nil {
					break
				}
				time.Sleep(1 * time.Second)
			}
			args = append(args, "--config", appFiles.TunnelYaml(), "run")
		} else {
			args = append(args, "--logfile", appFiles.BootLog(), "--loglevel", "info", "--url", "http://localhost:"+config.ArgoPort)
		}
	} else {
		args = append(args, "--logfile", appFiles.BootLog(), "--loglevel", "info", "--url", "http://localhost:"+config.ArgoPort)
	}
	if err := procMgr.Start("cloudflared", appFiles.Bot(), args, true); err != nil {
		logError("运行cloudflared失败: %v", err)
	}
}

func extractDomains() {
	if config.ArgoAuth != "" && config.ArgoDomain != "" {
		logInfo("使用固定域名: %s", config.ArgoDomain)
		generateLinks(config.ArgoDomain)
		return
	}
	data, err := os.ReadFile(appFiles.BootLog())
	if err != nil {
		logWarn("读取日志文件失败: %v", err)
		restartCloudflared()
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.Contains(line, "trycloudflare.com") {
			start := strings.Index(line, "https://")
			if start == -1 {
				start = strings.Index(line, "http://")
			}
			if start != -1 {
				end := strings.Index(line[start:], " ")
				if end == -1 {
					end = len(line) - start
				}
				url := line[start : start+end]
				domain := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
				domain = strings.TrimSuffix(domain, "/")
				logInfo("找到临时域名: %s", domain)
				generateLinks(domain)
				return
			}
		}
	}
	logWarn("未找到域名，重新运行cloudflared")
	restartCloudflared()
}

func restartCloudflared() {
	procMgr.Stop("cloudflared")
	time.Sleep(3 * time.Second)
	os.Remove(appFiles.BootLog())
	runCloudflared()
	time.Sleep(5 * time.Second)
	extractDomains()
}

func generateLinks(domain string) {
	isp := getISP()
	nodeName := config.Name
	if nodeName != "" {
		nodeName = nodeName + "-" + isp
	} else {
		nodeName = isp
	}

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
		"host": domain,
		"path": "/vmess-argo?ed=2560",
		"tls":  "tls",
		"sni":  domain,
		"alpn": "",
		"fp":   "firefox",
	}
	vmessJSON, _ := json.Marshal(vmessConfig)
	vmessBase64 := base64.StdEncoding.EncodeToString(vmessJSON)

	subTxt := fmt.Sprintf(`
vless://%s@%s:%s?encryption=none&security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Fvless-argo%%3Fed%%3D2560#%s

vmess://%s

trojan://%s@%s:%s?security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Ftrojan-argo%%3Fed%%3D2560#%s
`, config.UUID, config.CFIP, config.CFPort, domain, domain, nodeName,
		vmessBase64,
		config.UUID, config.CFIP, config.CFPort, domain, domain, nodeName)

	mu.Lock()
	subscription = subTxt
	mu.Unlock()

	encoded := base64.StdEncoding.EncodeToString([]byte(subTxt))
	if err := os.WriteFile(appFiles.Sub(), []byte(encoded), 0644); err != nil {
		logError("保存订阅文件失败: %v", err)
	} else {
		logInfo("订阅文件已保存: %s", appFiles.Sub())
	}
	logDebug("订阅base64内容:\n%s", encoded)
	uploadNodes()
}

// 修复 getISP 缓存为包级变量
var (
	ispCache struct {
		value string
		time  time.Time
	}
	ispCacheMu sync.Mutex
)

func getISP() string {
	ispCacheMu.Lock()
	defer ispCacheMu.Unlock()
	if time.Since(ispCache.time) < time.Hour {
		return ispCache.value
	}
	var data map[string]interface{}
	resp, err := httpClient.Get("https://ipapi.co/json/")
	if err == nil {
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			if country, ok := data["country_code"].(string); ok {
				if org, ok := data["org"].(string); ok {
					ispCache.value = country + "_" + org
					ispCache.time = time.Now()
					return ispCache.value
				}
			}
		}
	}
	resp, err = httpClient.Get("http://ip-api.com/json/")
	if err == nil {
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			if status, ok := data["status"].(string); ok && status == "success" {
				if country, ok := data["countryCode"].(string); ok {
					if org, ok := data["org"].(string); ok {
						ispCache.value = country + "_" + org
						ispCache.time = time.Now()
						return ispCache.value
					}
				}
			}
		}
	}
	return "Unknown"
}

func uploadNodes() {
	if config.UploadURL == "" {
		return
	}
	if config.ProjectURL != "" {
		subscriptionUrl := config.ProjectURL + "/" + config.SubPath
		jsonData := map[string][]string{"subscription": {subscriptionUrl}}
		data, _ := json.Marshal(jsonData)
		req, _ := http.NewRequest("POST", config.UploadURL+"/api/add-subscriptions", bytes.NewBuffer(data))
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			logWarn("订阅上传失败: %v", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			logInfo("订阅上传成功")
		} else if resp.StatusCode == 400 {
			logInfo("订阅已存在")
		} else {
			logWarn("订阅上传失败，状态码: %d", resp.StatusCode)
		}
	} else {
		data, err := os.ReadFile(appFiles.List())
		if err != nil {
			return
		}
		lines := strings.Split(string(data), "\n")
		var nodes []string
		for _, line := range lines {
			if isProxyLink(line) {
				nodes = append(nodes, line)
			}
		}
		if len(nodes) == 0 {
			return
		}
		jsonData, _ := json.Marshal(map[string][]string{"nodes": nodes})
		req, _ := http.NewRequest("POST", config.UploadURL+"/api/add-nodes", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		_, err = httpClient.Do(req)
		if err != nil {
			logWarn("节点上传失败: %v", err)
		} else {
			logInfo("节点上传成功")
		}
	}
}

func addVisitTask() {
	if !config.AutoAccess || config.ProjectURL == "" {
		logInfo("跳过自动访问任务")
		return
	}
	jsonData := map[string]string{"url": config.ProjectURL}
	data, _ := json.Marshal(jsonData)
	req, _ := http.NewRequest("POST", "https://oooo.serv00.net/add-url", bytes.NewBuffer(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		logWarn("添加自动访问任务失败: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		logInfo("自动访问任务添加成功")
	} else {
		logWarn("添加自动访问任务失败，状态码: %d", resp.StatusCode)
	}
}

func startMonitorScript() {
	if config.MonitorKey == "" || config.MonitorServer == "" || config.MonitorURL == "" {
		logInfo("监控环境变量不完整，跳过监控脚本启动")
		return
	}
	time.Sleep(10 * time.Second)
	logInfo("开始下载并运行监控脚本...")
	if err := downloadMonitorScript(); err != nil {
		logError("下载监控脚本失败: %v", err)
		return
	}
	if err := os.Chmod(appFiles.Monitor(), 0755); err != nil {
		logError("设置监控脚本执行权限失败: %v", err)
		return
	}
	go runMonitorScript()
}

func downloadMonitorScript() error {
	monitorURL := "https://raw.githubusercontent.com/mimaldq/cf-vps-monitor/main/cf-vps-monitor.sh"
	logDebug("从 %s 下载监控脚本", monitorURL)
	out, err := os.Create(appFiles.Monitor())
	if err != nil {
		return err
	}
	defer out.Close()
	resp, err := httpClient.Get(monitorURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: %s", resp.Status)
	}
	_, err = io.Copy(out, resp.Body)
	return err
}

func runMonitorScript() {
	args := []string{"-i", "-k", config.MonitorKey, "-s", config.MonitorServer, "-u", config.MonitorURL}
	logInfo("运行监控脚本: %s %s", appFiles.Monitor(), strings.Join(args, " "))
	cmd := exec.Command(appFiles.Monitor(), args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		logError("运行监控脚本失败: %v", err)
		return
	}
	logInfo("监控脚本启动成功（脚本自身将负责保活）")
	err := cmd.Wait()
	if err != nil {
		logWarn("监控脚本退出: %v", err)
	}
}

// ---------------------------- 信号处理与清理 ----------------------------
func setupSignalHandler() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		logInfo("收到关闭信号，正在清理...")
		procMgr.StopAll()
		cleanupFilesOnExit()
		logInfo("程序退出")
		os.Exit(0)
	}()
}

func cleanupFilesOnExit() {
	files := appFiles.AllTempFiles()
	for _, f := range files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			logWarn("删除文件失败 %s: %v", f, err)
		} else {
			logDebug("已删除临时文件: %s", f)
		}
	}
	logInfo("临时文件清理完成")
}
