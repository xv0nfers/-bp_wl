package v2ray_bridge

import (
	"encoding/json"
	"os"
)

type clientCfg struct {
	Inbounds  []map[string]any `json:"inbounds"`
	Outbounds []map[string]any `json:"outbounds"`
}

type serverCfg struct {
	Inbounds  []map[string]any `json:"inbounds"`
	Outbounds []map[string]any `json:"outbounds"`
}

func GenerateClient(path string) error {
	cfg := clientCfg{
		Inbounds: []map[string]any{
			{"protocol": "socks", "listen": "127.0.0.1", "port": 1080, "settings": map[string]any{"udp": true}},
			{"protocol": "http", "listen": "127.0.0.1", "port": 8080},
		},
		Outbounds: []map[string]any{
			{"protocol": "wireguard", "settings": map[string]any{"domainStrategy": "ForceIPv4", "mtu": 1280, "peers": []map[string]any{{"endpoint": "127.0.0.1:9000"}}}},
		},
	}
	return write(path, cfg)
}

func GenerateServer(path string) error {
	cfg := serverCfg{
		Inbounds: []map[string]any{
			{"protocol": "wireguard", "listen": "0.0.0.0", "port": 51820, "settings": map[string]any{"mtu": 1280}},
		},
		Outbounds: []map[string]any{{"protocol": "freedom", "settings": map[string]any{"domainStrategy": "UseIPv4"}}},
	}
	return write(path, cfg)
}

func write(path string, data any) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
