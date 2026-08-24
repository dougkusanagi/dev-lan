package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteProjectTable(t *testing.T) {
	var output bytes.Buffer
	rows := []projectRow{{
		Name: "cj-crm", Mode: "php", Source: "global",
		URL: "http://192.168.10.77/cj-crm", Path: "/home/silver/Sites/cj-crm",
	}}
	if err := writeProjectTable(&output, rows); err != nil {
		t.Fatal(err)
	}
	result := output.String()
	for _, expected := range []string{"PROJETO", "MODO", "ORIGEM", "URL", "CAMINHO", "cj-crm", "http://192.168.10.77/cj-crm", "/home/silver/Sites/cj-crm"} {
		if !strings.Contains(result, expected) {
			t.Fatalf("saída não contém %q:\n%s", expected, result)
		}
	}
}
