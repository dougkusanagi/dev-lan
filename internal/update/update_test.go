package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadVerifiedChecksChecksumBeforePublishing(t *testing.T) {
	payload := []byte("verified update")
	hash := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(payload)
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "devlan.new.exe")
	manifest := Manifest{Version: "1.2.3", Channel: Stable, URL: server.URL + "/devlan.exe", SHA256: hex.EncodeToString(hash[:]), Size: int64(len(payload))}
	if err := DownloadVerified(context.Background(), nil, manifest, Stable, target); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != string(payload) {
		t.Fatalf("artefato publicado incorretamente: %q %v", data, err)
	}
}

func TestDownloadVerifiedRejectsBadChecksum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("tampered"))
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "devlan.new.exe")
	err := DownloadVerified(context.Background(), nil, Manifest{Version: "1.2.3", Channel: Preview, URL: server.URL, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Preview, target)
	if err == nil {
		t.Fatal("checksum adulterado deveria falhar")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("artefato não deveria ser publicado: %v", statErr)
	}
}

func TestIsNewer(t *testing.T) {
	if !IsNewer("0.1.0", "0.2.0") || IsNewer("0.2.0", "0.1.0") || !IsNewer("0.1.0-mvp", "0.1.1") {
		t.Fatal("comparação de versões incorreta")
	}
}
