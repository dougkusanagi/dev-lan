package metrics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAggregateSanitizesAndGroupsAccessLog(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	data := []byte(`{"ts":1787659140,"duration":0.072,"status":200,"devlan_project":"shop","request":{"method":"GET","uri":"/orders/42?token=secret","remote_ip":"10.0.0.4"}}
{"ts":1787659141,"duration":0.512,"status":500,"devlan_project":"shop","request":{"method":"POST","uri":"/checkout","headers":{"Cookie":["secret"]}}}
{"ts":1787659142,"duration":0.100,"status":200,"devlan_project":"other","request":{"method":"GET","uri":"/"}}`)
	snapshot := Aggregate(data, "shop", Range1h, now)
	if snapshot == nil || snapshot.Requests != 2 || snapshot.ErrorCount != 1 {
		t.Fatalf("snapshot inesperado: %#v", snapshot)
	}
	if len(snapshot.Routes) != 2 || snapshot.Routes[0].NormalizedPath != "/checkout" {
		t.Fatalf("rotas inesperadas: %#v", snapshot.Routes)
	}
	if snapshot.Routes[1].NormalizedPath != "/orders/:id" {
		t.Fatalf("rota não normalizada: %#v", snapshot.Routes[1])
	}
}

func TestAggregateRequiresTrustedProjectMetadata(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	line := []byte(`{"ts":1787659140,"duration":0.1,"status":200,"request":{"method":"GET","uri":"/shop/private?token=secret"}}`)
	if got := Aggregate(line, "shop", Range1h, now); got != nil {
		t.Fatalf("URI não pode atribuir projeto: %#v", got)
	}
}

func TestAggregateStreamsLongLinesAndLimitsCardinality(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	var input strings.Builder
	for i := 0; i < 130; i++ {
		fmt.Fprintf(&input, `{"ts":1787659140,"duration":0.001,"status":200,"devlan_project":"shop","padding":"%s","request":{"method":"GET","uri":"/items/%d/%032x?secret=x"}}`+"\n", strings.Repeat("x", 70_000), i, i)
	}
	snapshot, err := AggregateReader(strings.NewReader(input.String()), "shop", Range1h, now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || snapshot.Requests != 130 {
		t.Fatalf("linhas longas não agregadas: %#v", snapshot)
	}
	if len(snapshot.Routes) > maxRouteCount {
		t.Fatalf("cardinalidade sem limite: %d", len(snapshot.Routes))
	}
	for _, route := range snapshot.Routes {
		if strings.Contains(route.NormalizedPath, "secret") || strings.Contains(route.NormalizedPath, "000000") {
			t.Fatalf("identificador/segredo vazou: %#v", route)
		}
	}
}

func TestCollectorHandlesCheckpointPartialTruncationAndRotation(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "access.jsonl")
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	line := `{"ts":1787659140,"duration":0.01,"status":200,"devlan_project":"shop","request":{"method":"GET","uri":"/one"}}`
	if err := os.WriteFile(active, []byte(line[:len(line)/2]), 0o600); err != nil {
		t.Fatal(err)
	}
	collector := NewCollector()
	if got, err := collector.Snapshot(active, "shop", Range1h, now); err != nil || got != nil {
		t.Fatalf("linha parcial: %#v, %v", got, err)
	}
	file, err := os.OpenFile(active, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString(line[len(line)/2:] + "\n")
	_ = file.Close()
	got, err := collector.Snapshot(active, "shop", Range1h, now)
	if err != nil || got == nil || got.Requests != 1 {
		t.Fatalf("checkpoint: %#v, %v", got, err)
	}
	if err := os.Rename(active, filepath.Join(dir, "access-old.jsonl")); err != nil {
		t.Fatal(err)
	}
	line2 := strings.Replace(line, "/one", "/two", 1)
	if err := os.WriteFile(active, []byte(line2+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = collector.Snapshot(active, "shop", Range1h, now)
	if err != nil || got.Requests != 2 {
		t.Fatalf("rotação: %#v, %v", got, err)
	}
	if err := os.WriteFile(active, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = collector.Snapshot(active, "shop", Range1h, now)
	if err != nil || got.Requests != 2 {
		t.Fatalf("truncamento/dedupe: %#v, %v", got, err)
	}
}

func TestAggregateReturnsNilWithoutSamples(t *testing.T) {
	now := time.Now()
	if got := Aggregate([]byte(`{"ts":1}`), "shop", Range1h, now); got != nil {
		t.Fatalf("esperava ausência de métricas, obtido %#v", got)
	}
}
