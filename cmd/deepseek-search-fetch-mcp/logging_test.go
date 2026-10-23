package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriter(t *testing.T) {
	dir := t.TempDir()
	w, err := newRotatingWriter(dir, "server.log", 256, 4)
	if err != nil {
		t.Fatalf("创建滚动写入器失败: %v", err)
	}
	defer w.Close()

	line := strings.Repeat("x", 63) + "\n"
	for i := 0; i < 40; i++ {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, "server.log")); err != nil {
		t.Fatalf("当前日志文件应存在: %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "server-*.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) == 0 {
		t.Fatal("应产生滚动备份文件")
	}
	if len(backups) > 4 {
		t.Fatalf("备份数量应不超过 4，实际 %d", len(backups))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > maxLogFiles {
		t.Fatalf("日志文件总数应不超过 %d，实际 %d", maxLogFiles, len(entries))
	}
}
