package detect

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLaravelFixture(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artisan"), []byte("#!/usr/bin/env php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "public", "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	detector := Detector{Inspector: LocalInspector{}}
	result, err := detector.Detect(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Artisan || !result.PublicIndex {
		t.Fatalf("detecção incorreta: %#v", result)
	}
}

func TestDetectRejectsIncompleteFixture(t *testing.T) {
	detector := Detector{Inspector: StaticInspector{Files: map[string]bool{"/project/artisan": true}}}
	_, err := detector.Detect(context.Background(), "/project")
	if err == nil {
		t.Fatal("fixture incompleta deveria ser rejeitada")
	}
}
