package main

import "testing"

func TestServiceInstallRequiresExplicitSystemConfirmation(t *testing.T) {
	if err := validateServiceInstallArgs([]string{"install"}); err == nil {
		t.Fatal("instalação LocalSystem sem confirmação deveria ser rejeitada")
	}
	if err := validateServiceInstallArgs([]string{"install", "--system"}); err != nil {
		t.Fatalf("instalação explicitamente sistêmica foi rejeitada: %v", err)
	}
}
