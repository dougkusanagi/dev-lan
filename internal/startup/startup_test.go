package startup

import (
	"strings"
	"testing"
)

func TestBuildCommandUsesInteractiveAPIFallbackForServiceMode(t *testing.T) {
	command, err := BuildCommand(`C:\Program Files\DevLAN\devlan.exe`, `C:\Users\dev\DevLAN`, ModeService)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(command, ` api serve`) || strings.Contains(command, `service run`) {
		t.Fatalf("comando de login incorreto: %s", command)
	}
	if _, err := BuildCommand(`C:\bad"path\devlan.exe`, `C:\data`, ModeGUI); err == nil {
		t.Fatal("aspas em caminho deveriam ser rejeitadas")
	}
}
