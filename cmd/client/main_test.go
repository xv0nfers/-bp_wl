package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseFlagsRequiresLink(t *testing.T) {
	_, err := parseFlagsFrom([]string{"-peer", "127.0.0.1:9999"})
	if err == nil {
		t.Fatal("expected error without link flag")
	}
}

func TestParseFlagsRequiresPeerWithoutDryRun(t *testing.T) {
	_, err := parseFlagsFrom([]string{"-vk-link", "https://vk.com/call/join/demo"})
	if err == nil {
		t.Fatal("expected error without peer")
	}
}

func TestGenerateV2JSON(t *testing.T) {
	dir := t.TempDir()
	clientPath := filepath.Join(dir, "client.json")
	serverPath := filepath.Join(dir, "server.json")

	_, err := parseFlagsFrom([]string{
		"-vk-link", "https://vk.com/call/join/demo",
		"-peer", "127.0.0.1:9999",
		"-gen-v2ray-client", clientPath,
		"-gen-v2ray-server", serverPath,
	})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	for _, path := range []string{clientPath, serverPath} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read generated file %s: %v", path, err)
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("invalid json in %s: %v", path, err)
		}
		if _, ok := out["inbounds"]; !ok {
			t.Fatalf("missing inbounds in %s", path)
		}
		if _, ok := out["outbounds"]; !ok {
			t.Fatalf("missing outbounds in %s", path)
		}
	}
}
