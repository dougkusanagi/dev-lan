package desktop

import (
	"context"
	"testing"
)

func TestDesktopInstallStatusUninstall(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("APPDATA", tempDir)

	ctx := context.Background()

	// Initial status -> not installed
	st, err := Status(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Installed {
		t.Fatal("inicialmente não deveria estar instalado")
	}

	// Install
	if err := Install(ctx, tempDir); err != nil {
		t.Fatal(err)
	}

	st, err = Status(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed {
		t.Fatal("deveria estar instalado após Install()")
	}

	// Uninstall
	if err := Uninstall(ctx, tempDir); err != nil {
		t.Fatal(err)
	}

	st, err = Status(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Installed {
		t.Fatal("não deveria estar instalado após Uninstall()")
	}
}

func TestCheckCompatibility(t *testing.T) {
	compat, _ := CheckCompatibility(1, 1)
	if !compat {
		t.Fatal("versões iguais deveriam ser compatíveis")
	}

	compat, detail := CheckCompatibility(1, 2)
	if compat {
		t.Fatal("versões diferentes deveriam ser incompatíveis")
	}
	if detail == "" {
		t.Fatal("deveria retornar detalhe acionável para versões incompatíveis")
	}
}
