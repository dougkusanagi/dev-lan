package metrics

import (
	"bufio"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Range string

const (
	Range15m Range = "15m"
	Range1h  Range = "1h"
	Range24h Range = "24h"
	Range7d  Range = "7d"
)

type Snapshot struct {
	Project            string          `json:"project"`
	Range              Range           `json:"range"`
	GeneratedAt        string          `json:"generatedAt"`
	ExcludedColdStarts int             `json:"excludedColdStarts"`
	Requests           int             `json:"requests"`
	RequestsPerMinute  float64         `json:"requestsPerMinute"`
	ErrorCount         int             `json:"errorCount"`
	ErrorRate          float64         `json:"errorRate"`
	P50Ms              *float64        `json:"p50Ms"`
	P95Ms              *float64        `json:"p95Ms"`
	LatencyBuckets     []Bucket        `json:"latencyBuckets"`
	Traffic            []TrafficPoint  `json:"traffic"`
	Routes             []RouteSnapshot `json:"routes"`
}

type Bucket struct {
	UpperBoundMs *int `json:"upperBoundMs"`
	Count        int  `json:"count"`
}

type TrafficPoint struct {
	At                string  `json:"at"`
	RequestsPerMinute float64 `json:"requestsPerMinute"`
}

type RouteSnapshot struct {
	Method         string   `json:"method"`
	NormalizedPath string   `json:"normalizedPath"`
	P50Ms          *float64 `json:"p50Ms"`
	P95Ms          *float64 `json:"p95Ms"`
	Requests       int      `json:"requests"`
	Errors         int      `json:"errors"`
}

type accessRecord struct {
	TS       float64 `json:"ts"`
	Duration float64 `json:"duration"`
	Status   int     `json:"status"`
	Request  struct {
		Method string `json:"method"`
		URI    string `json:"uri"`
	} `json:"request"`
}

type sample struct {
	at           time.Time
	duration     float64
	status       int
	method, path string
}

var idSegment = regexp.MustCompile(`^[0-9a-fA-F-]{8,}$|^[0-9]+$`)

// Aggregate parses only Caddy's method, URI path, status, timestamp and
// duration fields. Query strings, IPs, headers, cookies and bodies are never
// retained or returned to the UI.
func Aggregate(data []byte, project string, window Range, now time.Time) *Snapshot {
	cutoff, ok := cutoffFor(window, now)
	if !ok {
		return nil
	}
	samples := make([]sample, 0)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		var record accessRecord
		if json.Unmarshal(scanner.Bytes(), &record) != nil || record.TS <= 0 || record.Duration < 0 {
			continue
		}
		at := time.Unix(int64(record.TS), int64((record.TS-math.Floor(record.TS))*float64(time.Second)))
		if at.Before(cutoff) || at.After(now.Add(time.Minute)) {
			continue
		}
		path := strings.SplitN(record.Request.URI, "?", 2)[0]
		path = strings.TrimPrefix(path, "/")
		parts := strings.Split(path, "/")
		if len(parts) == 0 || parts[0] != project {
			continue
		}
		parts = parts[1:]
		clean := normalizePath(parts)
		samples = append(samples, sample{at: at, duration: record.Duration * 1000, status: record.Status, method: strings.ToUpper(record.Request.Method), path: clean})
	}
	if len(samples) == 0 {
		return nil
	}
	snapshot := &Snapshot{Project: project, Range: window, GeneratedAt: now.UTC().Format(time.RFC3339), LatencyBuckets: makeBuckets(samples)}
	snapshot.Requests = len(samples)
	for _, item := range samples {
		if item.status >= 400 {
			snapshot.ErrorCount++
		}
	}
	snapshot.ErrorRate = float64(snapshot.ErrorCount) / float64(snapshot.Requests) * 100
	snapshot.RequestsPerMinute = float64(snapshot.Requests) / windowMinutes(window)
	all := durations(samples)
	snapshot.P50Ms = percentile(all, .50)
	snapshot.P95Ms = percentile(all, .95)
	snapshot.Traffic = traffic(samples, cutoff, window)
	snapshot.Routes = routes(samples)
	return snapshot
}

func cutoffFor(window Range, now time.Time) (time.Time, bool) {
	switch window {
	case Range15m:
		return now.Add(-15 * time.Minute), true
	case Range1h:
		return now.Add(-time.Hour), true
	case Range24h:
		return now.Add(-24 * time.Hour), true
	case Range7d:
		return now.Add(-7 * 24 * time.Hour), true
	default:
		return time.Time{}, false
	}
}

func windowMinutes(window Range) float64 {
	switch window {
	case Range15m:
		return 15
	case Range1h:
		return 60
	case Range24h:
		return 1440
	default:
		return 10080
	}
}

func normalizePath(parts []string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if idSegment.MatchString(part) {
			part = ":id"
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "/"
	}
	return "/" + strings.Join(clean, "/")
}

func durations(samples []sample) []float64 {
	result := make([]float64, len(samples))
	for i, item := range samples {
		result[i] = item.duration
	}
	sort.Float64s(result)
	return result
}
func percentile(values []float64, ratio float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	index := int(math.Ceil(ratio*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	value := values[index]
	return &value
}

func makeBuckets(samples []sample) []Bucket {
	limits := []*int{intPtr(25), intPtr(50), intPtr(100), intPtr(250), intPtr(500), intPtr(1000), nil}
	result := make([]Bucket, len(limits))
	for i, limit := range limits {
		result[i].UpperBoundMs = limit
		lower := 0
		if i > 0 {
			lower = *limits[i-1]
		}
		for _, item := range samples {
			if limit == nil && item.duration > float64(lower) {
				result[i].Count++
			}
			if limit != nil && item.duration > float64(lower) && item.duration <= float64(*limit) {
				result[i].Count++
			}
		}
	}
	return result
}
func routes(samples []sample) []RouteSnapshot {
	groups := map[string][]sample{}
	for _, item := range samples {
		key := item.method + " " + item.path
		groups[key] = append(groups[key], item)
	}
	result := make([]RouteSnapshot, 0, len(groups))
	for _, group := range groups {
		values := durations(group)
		errors := 0
		for _, item := range group {
			if item.status >= 400 {
				errors++
			}
		}
		result = append(result, RouteSnapshot{Method: group[0].method, NormalizedPath: group[0].path, P50Ms: percentile(values, .5), P95Ms: percentile(values, .95), Requests: len(group), Errors: errors})
	}
	sort.Slice(result, func(i, j int) bool { return value(result[i].P95Ms) > value(result[j].P95Ms) })
	return result
}
func traffic(samples []sample, cutoff time.Time, window Range) []TrafficPoint {
	buckets := 12
	interval := time.Duration(windowMinutes(window)*60/float64(buckets)) * time.Second
	counts := make([]int, buckets)
	for _, item := range samples {
		index := int(item.at.Sub(cutoff) / interval)
		if index >= 0 && index < buckets {
			counts[index]++
		}
	}
	result := make([]TrafficPoint, buckets)
	for i, count := range counts {
		at := cutoff.Add(time.Duration(i) * interval)
		result[i] = TrafficPoint{At: at.Local().Format("15:04"), RequestsPerMinute: float64(count) / (interval.Minutes())}
	}
	return result
}
func value(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
func intPtr(value int) *int { return &value }
