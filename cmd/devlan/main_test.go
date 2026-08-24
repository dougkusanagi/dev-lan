package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteProjectTable(t *testing.T) {
	var output bytes.Buffer
	rows := []projectRow{{
		Name: "cj-crm", Mode: "php", Runtime: "8.5", Type: "laravel", Source: "global", SSL: "on",
		URL: "https://192.168.10.77/cj-crm", Path: "/home/silver/Sites/cj-crm",
	}}
	if err := writeProjectTable(&output, rows); err != nil {
		t.Fatal(err)
	}
	result := output.String()
	for _, expected := range []string{"PROJETO", "MODO", "RUNTIME", "TIPO", "ORIGEM", "SSL", "URL", "CAMINHO", "8.5", "laravel", "on", "cj-crm", "https://192.168.10.77/cj-crm", "/home/silver/Sites/cj-crm"} {
		if !strings.Contains(result, expected) {
			t.Fatalf("saída não contém %q:\n%s", expected, result)
		}
	}
}

func TestProjectRowJSON(t *testing.T) {
	row := projectRow{
		Name: "cj-crm", Mode: "php", Runtime: "8.5", Type: "laravel", Source: "global", SSL: "on",
		URL: "https://192.168.10.77/cj-crm", Path: "/home/silver/Sites/cj-crm",
	}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"name":"cj-crm"`, `"mode":"php"`, `"runtime":"8.5"`, `"type":"laravel"`, `"source":"global"`, `"ssl":"on"`, `"url":"https://192.168.10.77/cj-crm"`, `"path":"/home/silver/Sites/cj-crm"`} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("JSON não contém %q:\n%s", expected, string(data))
		}
	}
}
