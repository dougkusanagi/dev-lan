package metrics

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Range string

const (
	Range15m      Range = "15m"
	Range1h       Range = "1h"
	Range24h      Range = "24h"
	Range7d       Range = "7d"
	maxLogFiles         = 4
	maxLineBytes        = 1 << 20
	maxReadBytes        = 48 << 20
	maxSamples          = 100_000
	maxRouteCount       = 100
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

// Only allowlisted fields are decoded. devlan_project is set by the managed
// Caddy route, so attribution never depends on a user-controlled URI or host.
type accessRecord struct {
	TS       float64 `json:"ts"`
	Duration float64 `json:"duration"`
	Status   int     `json:"status"`
	Project  string  `json:"devlan_project"`
	Request  struct {
		Method string `json:"method"`
		URI    string `json:"uri"`
	} `json:"request"`
}

type sample struct {
	at                         time.Time
	duration                   float64
	status                     int
	method, path, project, key string
}
type checkpoint struct {
	offset, size int64
	modTime      time.Time
	head         string
}

// Collector follows the active log and its managed rotations incrementally.
// Memory, files, bytes per poll and route cardinality are all explicitly bounded.
type Collector struct {
	mu          sync.Mutex
	checkpoints map[string]checkpoint
	samples     []sample
	seen        map[string]struct{}
}

func NewCollector() *Collector {
	return &Collector{checkpoints: map[string]checkpoint{}, seen: map[string]struct{}{}}
}

var (
	numericSegment = regexp.MustCompile(`^[0-9]+$`)
	uuidSegment    = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	hexSegment     = regexp.MustCompile(`(?i)^[0-9a-f]{16,}$`)
	ulidSegment    = regexp.MustCompile(`(?i)^[0-9a-hjkmnp-tv-z]{26}$`)
	tokenSegment   = regexp.MustCompile(`^[A-Za-z0-9_-]{32,}$`)
)

// Aggregate keeps compatibility for in-memory callers while using the
// streaming parser. Records without trusted project metadata are ignored.
func Aggregate(data []byte, project string, window Range, now time.Time) *Snapshot {
	items, _ := parseReader(strings.NewReader(string(data)), maxReadBytes)
	return aggregateSamples(items, project, window, now)
}

// AggregateReader supports records beyond bufio.Scanner's 64 KiB limit while
// discarding malformed or over-one-megabyte lines.
func AggregateReader(reader io.Reader, project string, window Range, now time.Time) (*Snapshot, error) {
	items, err := parseReader(reader, maxReadBytes)
	if err != nil {
		return nil, err
	}
	return aggregateSamples(items, project, window, now), nil
}

func (c *Collector) Snapshot(activePath, project string, window Range, now time.Time) (*Snapshot, error) {
	if c == nil {
		return nil, errors.New("collector de métricas não configurado")
	}
	if _, ok := cutoffFor(window, now); !ok {
		return nil, fmt.Errorf("intervalo de métricas inválido: %s", window)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	files, err := logFiles(activePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	for _, name := range files {
		if err := c.readAppended(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	c.prune(now.Add(-7 * 24 * time.Hour))
	return aggregateSamples(c.samples, project, window, now), nil
}

func logFiles(activePath string) ([]string, error) {
	extension := filepath.Ext(activePath)
	matches, err := filepath.Glob(strings.TrimSuffix(activePath, extension) + "*" + extension + "*")
	if err != nil {
		return nil, err
	}
	type candidate struct {
		path string
		info os.FileInfo
	}
	items := make([]candidate, 0, len(matches))
	for _, match := range matches {
		if info, statErr := os.Stat(match); statErr == nil && !info.IsDir() {
			items = append(items, candidate{match, info})
		}
	}
	if len(items) == 0 {
		return nil, os.ErrNotExist
	}
	sort.Slice(items, func(i, j int) bool { return items[i].info.ModTime().After(items[j].info.ModTime()) })
	if len(items) > maxLogFiles {
		items = items[:maxLogFiles]
	}
	result := make([]string, len(items))
	for i := range items {
		result[len(items)-1-i] = items[i].path
	}
	return result, nil
}

func (c *Collector) readAppended(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	cp := c.checkpoints[path]
	compressed := strings.HasSuffix(strings.ToLower(path), ".gz")
	if compressed && cp.size == info.Size() && cp.modTime.Equal(info.ModTime()) {
		return nil
	}
	if !compressed && info.Size() < cp.offset {
		cp = checkpoint{}
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	head, err := fileHead(file)
	if err != nil {
		return err
	}
	if !compressed && cp.offset > 0 && cp.head != "" && cp.head != head {
		cp = checkpoint{}
	}
	var reader io.Reader = file
	start := cp.offset
	if compressed {
		gz, gzipErr := gzip.NewReader(file)
		if gzipErr != nil {
			return gzipErr
		}
		defer gz.Close()
		reader, start = gz, 0
	} else if start > 0 {
		if _, err := file.Seek(start, io.SeekStart); err != nil {
			return err
		}
	}
	parsed, consumed, err := parseIncrement(reader, maxReadBytes)
	if err != nil {
		return err
	}
	for _, item := range parsed {
		if _, exists := c.seen[item.key]; exists {
			continue
		}
		c.seen[item.key] = struct{}{}
		c.samples = append(c.samples, item)
	}
	if compressed {
		c.checkpoints[path] = checkpoint{size: info.Size(), modTime: info.ModTime(), head: head}
	} else {
		c.checkpoints[path] = checkpoint{offset: start + consumed, size: info.Size(), modTime: info.ModTime(), head: head}
	}
	return nil
}

func fileHead(file *os.File) (string, error) {
	buffer := make([]byte, 256)
	n, err := file.ReadAt(buffer, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return string(buffer[:n]), nil
}

func (c *Collector) prune(cutoff time.Time) {
	kept := c.samples[:0]
	for _, item := range c.samples {
		if !item.at.Before(cutoff) {
			kept = append(kept, item)
		}
	}
	if len(kept) > maxSamples {
		kept = kept[len(kept)-maxSamples:]
	}
	c.samples = kept
	c.seen = make(map[string]struct{}, len(kept))
	for _, item := range kept {
		c.seen[item.key] = struct{}{}
	}
}

func parseReader(reader io.Reader, limit int64) ([]sample, error) {
	items, _, err := parseIncrement(reader, limit)
	return items, err
}

func parseIncrement(reader io.Reader, limit int64) ([]sample, int64, error) {
	limited := &io.LimitedReader{R: reader, N: limit + 1}
	buffered := bufio.NewReaderSize(limited, 64*1024)
	result := make([]sample, 0)
	var consumed int64
	for {
		line, err := buffered.ReadString('\n')
		complete := strings.HasSuffix(line, "\n")
		if complete {
			consumed += int64(len(line))
		}
		if complete && len(line) <= maxLineBytes {
			if item, ok := parseLine([]byte(strings.TrimSuffix(line, "\n"))); ok {
				result = append(result, item)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return result, consumed, err
		}
		if limited.N <= 0 {
			return result, consumed, fmt.Errorf("limite incremental de %d bytes excedido", limit)
		}
	}
	return result, consumed, nil
}

func parseLine(line []byte) (sample, bool) {
	var record accessRecord
	if json.Unmarshal(line, &record) != nil || record.TS <= 0 || record.Duration < 0 || !validProject(record.Project) {
		return sample{}, false
	}
	at := time.Unix(int64(record.TS), int64((record.TS-math.Floor(record.TS))*float64(time.Second)))
	uriPath := strings.SplitN(record.Request.URI, "?", 2)[0]
	method, clean := normalizedMethod(record.Request.Method), normalizePath(strings.Split(strings.TrimPrefix(uriPath, "/"), "/"))
	key := fmt.Sprintf("%.9f|%.9f|%d|%s|%s|%s", record.TS, record.Duration, record.Status, record.Project, method, clean)
	return sample{at: at, duration: record.Duration * 1000, status: record.Status, method: method, path: clean, project: record.Project, key: key}, true
}

func validProject(project string) bool {
	if project == "" || len(project) > 63 {
		return false
	}
	for _, r := range project {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func normalizedMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		return method
	}
	return "OTHER"
}

func aggregateSamples(all []sample, project string, window Range, now time.Time) *Snapshot {
	cutoff, ok := cutoffFor(window, now)
	if !ok || !validProject(project) {
		return nil
	}
	items := make([]sample, 0)
	for _, item := range all {
		if item.project == project && !item.at.Before(cutoff) && !item.at.After(now.Add(time.Minute)) {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil
	}
	result := &Snapshot{Project: project, Range: window, GeneratedAt: now.UTC().Format(time.RFC3339), LatencyBuckets: makeBuckets(items), Requests: len(items)}
	for _, item := range items {
		if item.status >= 400 {
			result.ErrorCount++
		}
	}
	result.ErrorRate = float64(result.ErrorCount) / float64(result.Requests) * 100
	result.RequestsPerMinute = float64(result.Requests) / windowMinutes(window)
	values := durations(items)
	result.P50Ms, result.P95Ms = percentile(values, .50), percentile(values, .95)
	result.Traffic, result.Routes = traffic(items, cutoff, window), routes(items)
	return result
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
	}
	return time.Time{}, false
}
func windowMinutes(window Range) float64 {
	switch window {
	case Range15m:
		return 15
	case Range1h:
		return 60
	case Range24h:
		return 1440
	}
	return 10080
}

func normalizePath(parts []string) string {
	clean := make([]string, 0, min(len(parts), 12))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if len(clean) == 12 {
			clean = append(clean, ":rest")
			break
		}
		if dynamicSegment(part) {
			part = ":id"
		} else if len(part) > 64 {
			part = part[:64]
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "/"
	}
	return "/" + strings.Join(clean, "/")
}
func dynamicSegment(part string) bool {
	return numericSegment.MatchString(part) || uuidSegment.MatchString(part) || hexSegment.MatchString(part) || ulidSegment.MatchString(part) || tokenSegment.MatchString(part)
}

func durations(items []sample) []float64 {
	result := make([]float64, len(items))
	for i, item := range items {
		result[i] = item.duration
	}
	sort.Float64s(result)
	return result
}
func percentile(values []float64, ratio float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	i := int(math.Ceil(ratio*float64(len(values)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(values) {
		i = len(values) - 1
	}
	value := values[i]
	return &value
}
func makeBuckets(items []sample) []Bucket {
	limits := []*int{intPtr(25), intPtr(50), intPtr(100), intPtr(250), intPtr(500), intPtr(1000), nil}
	result := make([]Bucket, len(limits))
	for i, limit := range limits {
		result[i].UpperBoundMs = limit
		lower := 0
		if i > 0 {
			lower = *limits[i-1]
		}
		for _, item := range items {
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
func routes(items []sample) []RouteSnapshot {
	groups := map[string][]sample{}
	for _, item := range items {
		key := item.method + " " + item.path
		groups[key] = append(groups[key], item)
	}
	type group struct {
		key   string
		items []sample
	}
	ordered := make([]group, 0, len(groups))
	for key, items := range groups {
		ordered = append(ordered, group{key, items})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if len(ordered[i].items) == len(ordered[j].items) {
			return ordered[i].key < ordered[j].key
		}
		return len(ordered[i].items) > len(ordered[j].items)
	})
	if len(ordered) > maxRouteCount {
		rest := make([]sample, 0)
		for _, entry := range ordered[maxRouteCount-1:] {
			rest = append(rest, entry.items...)
		}
		for i := range rest {
			rest[i].method, rest[i].path = "OTHER", "/:other"
		}
		ordered = append(ordered[:maxRouteCount-1], group{"OTHER /:other", rest})
	}
	result := make([]RouteSnapshot, 0, len(ordered))
	for _, entry := range ordered {
		values := durations(entry.items)
		errors := 0
		for _, item := range entry.items {
			if item.status >= 400 {
				errors++
			}
		}
		result = append(result, RouteSnapshot{Method: entry.items[0].method, NormalizedPath: entry.items[0].path, P50Ms: percentile(values, .5), P95Ms: percentile(values, .95), Requests: len(entry.items), Errors: errors})
	}
	sort.Slice(result, func(i, j int) bool {
		if value(result[i].P95Ms) == value(result[j].P95Ms) {
			return result[i].Method+result[i].NormalizedPath < result[j].Method+result[j].NormalizedPath
		}
		return value(result[i].P95Ms) > value(result[j].P95Ms)
	})
	return result
}
func traffic(items []sample, cutoff time.Time, window Range) []TrafficPoint {
	buckets := 12
	interval := time.Duration(windowMinutes(window)*60/float64(buckets)) * time.Second
	counts := make([]int, buckets)
	for _, item := range items {
		i := int(item.at.Sub(cutoff) / interval)
		if i >= 0 && i < buckets {
			counts[i]++
		}
	}
	result := make([]TrafficPoint, buckets)
	for i, count := range counts {
		result[i] = TrafficPoint{At: cutoff.Add(time.Duration(i) * interval).Local().Format("15:04"), RequestsPerMinute: float64(count) / interval.Minutes()}
	}
	return result
}
func value(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
func intPtr(v int) *int { return &v }
