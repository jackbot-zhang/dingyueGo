package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Proxy struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Server   string `yaml:"server"`
	Port     int    `yaml:"port"`
	Cipher   string `yaml:"cipher,omitempty"`
	Password string `yaml:"password,omitempty"`
	UUID     string `yaml:"uuid,omitempty"`
	AlterID  string `yaml:"alterId,omitempty"`
	Network  string `yaml:"network,omitempty"`
}

type vmess struct {
	Ps   string `json:"ps"`
	Port string `json:"port"`
	Id   string `json:"id"`
	Aid  int    `json:"aid"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Tls  string `json:"tls"`
	Add  string `json:"add"`
}

func main() {
	data, err := os.ReadFile("/home/zhang/.config/clash/profiles/1681350197793.yml")
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	fmt.Println("读取配置文件成功")
	t := make(map[interface{}]interface{})
	err = yaml.Unmarshal(data, &t)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	t["proxies"] = proxys()
	ln, _ := yaml.Marshal(t)
	err = os.WriteFile("/home/zhang/.config/clash/profiles/1681350197793.yml", ln, os.ModePerm)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	fmt.Println("写入配置文件成功")
	time.Sleep(500 * time.Millisecond)
}
func proxys() []Proxy {
	resp, err := http.Get("https://jmssub.net/members/getsub.php?service=131783&id=a35aa5ea-5893-41bc-86c9-5d283bd9cd68")
	if err != nil {
		panic(err.Error())
	}
	defer resp.Body.Close()
	all, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err.Error())
	}

	url := string(all)

	str, _ := base64.StdEncoding.DecodeString(url)
	arr := strings.Split(string(str), "\n")

	sscnt := 1
	vmesscnt := 1

	var proxies []Proxy

	for _, v := range arr {
		v = strings.TrimSpace(v)

		if strings.HasPrefix(v, "ss://") {
			v = strings.TrimPrefix(v, "ss://")
			x, _ := base64.RawStdEncoding.DecodeString(strings.Split(v, "#")[0])

			tmp := strings.Split(string(x), "@")
			a := strings.Split(tmp[0], ":")
			b := strings.Split(tmp[1], ":")
			port, _ := strconv.Atoi(b[1])
			proxy := Proxy{
				Name:     fmt.Sprintf("ss%d", sscnt),
				Type:     "ss",
				Server:   b[0],
				Port:     port,
				Cipher:   a[0],
				Password: a[1],
			}

			proxies = append(proxies, proxy)

			sscnt++
		} else {
			v = strings.TrimPrefix(v, "vmess://")
			x, _ := base64.RawStdEncoding.DecodeString(v)

			var j vmess
			_ = yaml.Unmarshal(x, &j)
			port, _ := strconv.Atoi(j.Port)
			proxy := Proxy{
				Name:    fmt.Sprintf("vmess%d", vmesscnt),
				Type:    "vmess",
				Server:  j.Add,
				Port:    port,
				UUID:    j.Id,
				AlterID: "0",
				Cipher:  "auto",
				Network: "tcp",
			}

			proxies = append(proxies, proxy)

			vmesscnt++
		}
	}
	return proxies

}
