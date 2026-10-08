package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ============================================================
// 常量
// ============================================================

// 文件路径
const (
	clashOutputPath = "/home/zhang/.local/share/io.github.clash-verge-rev.clash-verge-rev/profiles/LzXDsMNuCFNA.yaml"
)

// 网络地址和超时
const (
	subscriptionURL = "https://jmssub.net/members/getsub.php?service=1470122&id=fc741094-9854-4a75-9529-6e5c4ef712fe"
	clashAPIBaseURL = "http://127.0.0.1:9097"
	clashAPISecret  = "set-your-secret"
	httpTimeout     = 5 * time.Second
)

// ============================================================
// 数据类型
// ============================================================

// RealityOpts 是 Reality 协议的专属参数
type RealityOpts struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id"`
}

// Proxy 表示一个 Clash 代理节点
type Proxy struct {
	Name              string      `yaml:"name"`
	Type              string      `yaml:"type"`
	Server            string      `yaml:"server"`
	Port              int         `yaml:"port"`
	Cipher            string      `yaml:"cipher,omitempty"`
	Password          string      `yaml:"password,omitempty"`
	UUID              string      `yaml:"uuid,omitempty"`
	AlterID           string      `yaml:"alterId,omitempty"`
	Network           string      `yaml:"network,omitempty"`
	TLS               bool        `yaml:"tls,omitempty"`
	UDP               bool        `yaml:"udp,omitempty"`
	Flow              string      `yaml:"flow,omitempty"`
	ClientFingerprint string      `yaml:"client-fingerprint,omitempty"`
	Servername        string      `yaml:"servername,omitempty"`
	RealityOpts       RealityOpts `yaml:"reality-opts,omitempty"`
}

// vmessLink 用于解析 vmess:// 链接中的 JSON 内容
type vmessLink struct {
	Ps   string `json:"ps"`
	Port string `json:"port"`
	Id   string `json:"id"`
	Aid  int    `json:"aid"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Tls  string `json:"tls"`
	Add  string `json:"add"`
}

// ============================================================
// 主流程
// ============================================================

func main() {
	// 1. 读取现有 Clash 配置文件
	data, err := os.ReadFile(clashOutputPath)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	logInfo("读取配置文件成功")

	// 2. 解析为通用 map，保留原有结构
	cfg := make(map[interface{}]interface{})
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("error: %v", err)
	}

	// 3. 从订阅地址获取代理列表
	proxies := fetchProxies()
	cfg["proxies"] = proxies

	// 4. 同步更新 proxy-groups 中的 proxies 列表
	proxyNames := collectProxyNames(proxies)
	updateProxyGroups(cfg, proxyNames)

	// 5. 写回配置文件
	output, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(clashOutputPath, output, os.ModePerm); err != nil {
		log.Fatalf("error: %v", err)
	}
	logInfo("写入配置文件成功")

	// 6. 通过 Clash API 热重载
	if err := reloadClash(output); err != nil {
		fmt.Printf("重载配置失败: %v\n", err)
	} else {
		logInfo("重载配置成功")
	}

	time.Sleep(500 * time.Millisecond)
}

// logInfo 打印带时间戳的日志信息
func logInfo(msg string) {
	fmt.Printf("%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
}

// ============================================================
// 订阅解析
// ============================================================

// fetchProxies 从订阅地址获取并解析所有代理节点
func fetchProxies() []Proxy {
	resp, err := http.Get(subscriptionURL)
	if err != nil {
		log.Fatalf("获取订阅失败: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("读取订阅响应失败: %v", err)
	}

	// 订阅内容整体是 base64 编码
	decoded, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		log.Fatalf("base64 解码失败: %v", err)
	}

	lines := strings.Split(string(decoded), "\n")
	ssCount := 1
	vmessCount := 1
	vlessCount := 1

	var proxies []Proxy

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "ss://"):
			proxy, ok := parseSS(line, ssCount)
			if ok {
				proxies = append(proxies, proxy)
				ssCount++
			}

		case strings.HasPrefix(line, "vless://"):
			proxy, err := parseVless(line, vlessCount)
			if err != nil {
				fmt.Printf("解析 vless 失败: %v\n", err)
				continue
			}
			proxies = append(proxies, *proxy)
			vlessCount++

		case strings.HasPrefix(line, "vmess://"):
			proxy, ok := parseVmess(line, vmessCount)
			if ok {
				proxies = append(proxies, proxy)
				vmessCount++
			}
		}
	}

	return proxies
}

// parseSS 解析 ss:// 链接为 Proxy
func parseSS(raw string, index int) (Proxy, bool) {
	raw = strings.TrimPrefix(raw, "ss://")

	// 格式: base64(method:password)@host:port#备注
	parts := strings.SplitN(raw, "#", 2)
	decoded, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil {
		return Proxy{}, false
	}

	credentials := strings.SplitN(string(decoded), "@", 2)
	if len(credentials) != 2 {
		return Proxy{}, false
	}

	auth := strings.SplitN(credentials[0], ":", 2)
	if len(auth) != 2 {
		return Proxy{}, false
	}

	addr := strings.SplitN(credentials[1], ":", 2)
	if len(addr) != 2 {
		return Proxy{}, false
	}

	port, err := strconv.Atoi(addr[1])
	if err != nil {
		return Proxy{}, false
	}

	return Proxy{
		Name:     fmt.Sprintf("ss%d", index),
		Type:     "ss",
		Server:   addr[0],
		Port:     port,
		Cipher:   auth[0],
		Password: auth[1],
	}, true
}

// parseVmess 解析 vmess:// 链接为 Proxy
func parseVmess(raw string, index int) (Proxy, bool) {
	raw = strings.TrimPrefix(raw, "vmess://")

	decoded, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		return Proxy{}, false
	}

	var link vmessLink
	if err := json.Unmarshal(decoded, &link); err != nil {
		return Proxy{}, false
	}

	port, err := strconv.Atoi(link.Port)
	if err != nil {
		return Proxy{}, false
	}

	return Proxy{
		Name:    fmt.Sprintf("vmess%d", index),
		Type:    "vmess",
		Server:  link.Add,
		Port:    port,
		UUID:    link.Id,
		AlterID: "0",
		Cipher:  "auto",
		Network: "tcp",
	}, true
}

// parseVless 解析 vless:// URL 为 Proxy
func parseVless(rawURL string, index int) (*Proxy, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("无效的 URL: %w", err)
	}
	if u.Scheme != "vless" {
		return nil, fmt.Errorf("协议不是 vless，当前为: %s", u.Scheme)
	}

	// 1. 提取 UUID
	if u.User == nil {
		return nil, fmt.Errorf("缺少 UUID")
	}
	uuid := u.User.Username()
	if uuid == "" {
		return nil, fmt.Errorf("UUID 为空")
	}

	// 2. 提取服务器地址和端口
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return nil, fmt.Errorf("无效的 host:port: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("端口不是数字: %w", err)
	}

	// 3. 解析查询参数
	q := u.Query()
	network := q.Get("type")
	if network == "" {
		network = "tcp"
	}

	security := q.Get("security")
	tls := (security == "reality" || security == "tls")

	udp := true
	if udpVal := q.Get("udp"); udpVal != "" {
		if b, err := strconv.ParseBool(udpVal); err == nil {
			udp = b
		}
	}

	return &Proxy{
		Name:              fmt.Sprintf("vless%d", index),
		Type:              "vless",
		Server:            host,
		Port:              port,
		UUID:              uuid,
		Network:           network,
		TLS:               tls,
		UDP:               udp,
		Flow:              q.Get("flow"),
		ClientFingerprint: q.Get("fp"),
		Servername:        q.Get("sni"),
		RealityOpts: RealityOpts{
			PublicKey: q.Get("pbk"),
			ShortID:   q.Get("sid"),
		},
	}, nil
}

// ============================================================
// proxy-groups 同步
// ============================================================

// collectProxyNames 从代理列表中提取所有代理名称
func collectProxyNames(proxies []Proxy) []string {
	names := make([]string, 0, len(proxies))
	for _, p := range proxies {
		names = append(names, p.Name)
	}
	return names
}

// updateProxyGroups 更新配置中所有 proxy-group 的 proxies 列表
// 保留每个 group 的原有结构，仅替换 proxies 字段为最新的代理名列表
func updateProxyGroups(cfg map[interface{}]interface{}, proxyNames []string) {
	rawGroups, ok := cfg["proxy-groups"]
	if !ok {
		return
	}

	groups, ok := rawGroups.([]interface{})
	if !ok {
		return
	}

	for _, rawGroup := range groups {
		group, ok := rawGroup.(map[string]interface{})
		if !ok {
			continue
		}

		// 判断旧列表是否以 auto 开头（即 PROXY 这类 select 组）。
		// 如果是，直接覆盖为 auto + 最新节点列表；否则直接覆盖为最新节点列表。
		if startsWithAuto(group) {
			group["proxies"] = append([]string{"auto"}, proxyNames...)
			continue
		}

		// 其余组直接覆盖为最新的节点列表
		group["proxies"] = proxyNames
	}
}

// startsWithAuto 判断该 proxy-group 的原有 proxies 列表是否以 "auto" 开头
func startsWithAuto(group map[string]interface{}) bool {
	raw, exists := group["proxies"]
	if !exists {
		return false
	}

	slice, ok := raw.([]interface{})
	if !ok || len(slice) == 0 {
		return false
	}

	first, ok := slice[0].(string)
	return ok && first == "auto"
}

// ============================================================
// Clash API 操作
// ============================================================

// reloadClash 通过 Clash API 热重载配置
func reloadClash(cf []byte) error {
	// 读取运行时配置
	body := struct {
		Path    string `json:"path"`
		Payload string `json:"payload"`
	}{Payload: string(cf)}
	payload, _ := json.Marshal(body)

	req, err := http.NewRequest(
		http.MethodPut,
		clashAPIBaseURL+"/configs?force=true",
		bytes.NewBuffer(payload),
	)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+clashAPISecret)

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API 返回错误 %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
