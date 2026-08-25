package metrics

import (
	"testing"
	"time"
)

func TestAggregateSanitizesAndGroupsAccessLog(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	data := []byte(`{"ts":1787659140,"duration":0.072,"status":200,"request":{"method":"GET","uri":"/shop/orders/42?token=secret","remote_ip":"10.0.0.4"}}
{"ts":1787659141,"duration":0.512,"status":500,"request":{"method":"POST","uri":"/shop/checkout","headers":{"Cookie":["secret"]}}}
{"ts":1787659142,"duration":0.100,"status":200,"request":{"method":"GET","uri":"/other"}}`)
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

func TestAggregateReturnsNilWithoutSamples(t *testing.T) {
	now := time.Now()
	if got := Aggregate([]byte(`{"ts":1}`), "shop", Range1h, now); got != nil {
		t.Fatalf("esperava ausência de métricas, obtido %#v", got)
	}
}
