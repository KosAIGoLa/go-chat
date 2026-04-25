package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`service:
  name: test
  env: unit
http:
  addr: ":18080"
redis:
  addrs: ["127.0.0.1:6379"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Service.Name != "test" || cfg.HTTP.Addr != ":18080" || len(cfg.Redis.Addrs) != 1 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}
