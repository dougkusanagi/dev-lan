package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var wslConfigMu sync.Mutex

// WSLConfigSettings is the small, supported subset of the user-owned
// .wslconfig that DevLAN manages. The editor below deliberately does not
// deserialize the whole file: unknown keys, sections and comments must remain
// byte-for-byte intact whenever possible.
type WSLConfigSettings struct {
	NetworkingMode      string
	Firewall            string
	DNSTunneling        string
	AutoProxy           string
	HostAddressLoopback string
}

func DefaultWSLConfigSettings() WSLConfigSettings {
	return WSLConfigSettings{
		NetworkingMode:      "mirrored",
		Firewall:            "true",
		DNSTunneling:        "true",
		AutoProxy:           "true",
		HostAddressLoopback: "true",
	}
}

type WSLConfigUpdate struct {
	Path       string `json:"path"`
	BackupPath string `json:"backupPath,omitempty"`
	Changed    bool   `json:"changed"`
}

// UserWSLConfigPath returns the host-level WSL configuration path. USERPROFILE
// is preferred because it is the documented Windows location and is also easy
// to inject in tests. On other systems it falls back to the conventional home
// directory so the text editor remains testable without Windows.
func UserWSLConfigPath() string {
	if profile := strings.TrimSpace(os.Getenv("USERPROFILE")); profile != "" {
		return filepath.Join(profile, ".wslconfig")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".wslconfig")
	}
	return ".wslconfig"
}

func (s WSLConfigSettings) normalized() (WSLConfigSettings, error) {
	defaults := DefaultWSLConfigSettings()
	if strings.TrimSpace(s.NetworkingMode) == "" {
		s.NetworkingMode = defaults.NetworkingMode
	}
	if strings.TrimSpace(s.Firewall) == "" {
		s.Firewall = defaults.Firewall
	}
	if strings.TrimSpace(s.DNSTunneling) == "" {
		s.DNSTunneling = defaults.DNSTunneling
	}
	if strings.TrimSpace(s.AutoProxy) == "" {
		s.AutoProxy = defaults.AutoProxy
	}
	if strings.TrimSpace(s.HostAddressLoopback) == "" {
		s.HostAddressLoopback = defaults.HostAddressLoopback
	}
	s.NetworkingMode = strings.ToLower(strings.TrimSpace(s.NetworkingMode))
	for name, value := range map[string]string{
		"firewall":            s.Firewall,
		"dnsTunneling":        s.DNSTunneling,
		"autoProxy":           s.AutoProxy,
		"hostAddressLoopback": s.HostAddressLoopback,
	} {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "true" && value != "false" {
			return WSLConfigSettings{}, fmt.Errorf("%s deve ser true ou false", name)
		}
		switch name {
		case "firewall":
			s.Firewall = value
		case "dnsTunneling":
			s.DNSTunneling = value
		case "autoProxy":
			s.AutoProxy = value
		case "hostAddressLoopback":
			s.HostAddressLoopback = value
		}
	}
	if s.NetworkingMode != "mirrored" {
		return WSLConfigSettings{}, fmt.Errorf("networkingMode deve ser mirrored")
	}
	return s, nil
}

// UpdateWSLConfigText edits only [wsl2]. It recognizes keys case-insensitively,
// updates the last active occurrence in that section, and inserts a missing
// key before the next section. Commented-out keys are not treated as active
// settings. The original newline convention is retained.
func UpdateWSLConfigText(input string, settings WSLConfigSettings) (string, error) {
	settings, err := settings.normalized()
	if err != nil {
		return "", err
	}
	hasBOM := strings.HasPrefix(input, "\ufeff")
	if hasBOM {
		input = strings.TrimPrefix(input, "\ufeff")
	}
	newline := "\n"
	if strings.Contains(input, "\r\n") {
		newline = "\r\n"
	}
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	hadTrailingNewline := strings.HasSuffix(normalized, "\n")
	lines := strings.Split(normalized, "\n")
	if hadTrailingNewline && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	sectionStart, sectionEnd := -1, len(lines)
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 2 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
			continue
		}
		section := strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
		if section == "wsl2" && sectionStart < 0 {
			sectionStart = index
			continue
		}
		if sectionStart >= 0 {
			sectionEnd = index
			break
		}
	}
	if sectionStart < 0 {
		// strings.Split("", "\n") returns one empty line. Do not turn an
		// empty file into a file that starts with an unexplained blank line.
		if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
			lines = lines[:0]
		}
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "[wsl2]")
		sectionStart = len(lines) - 1
		sectionEnd = len(lines)
	}

	updates := []struct {
		key   string
		value string
	}{
		{"networkingMode", settings.NetworkingMode},
		{"firewall", settings.Firewall},
		{"dnsTunneling", settings.DNSTunneling},
		{"autoProxy", settings.AutoProxy},
	}
	lastAssignment := make(map[string]int, len(updates))
	for index := sectionStart + 1; index < sectionEnd; index++ {
		key, _, _, ok := parseWSLConfigAssignment(lines[index])
		if !ok {
			continue
		}
		for _, item := range updates {
			if !strings.EqualFold(key, item.key) {
				continue
			}
			lastAssignment[item.key] = index
		}
	}
	updated := make(map[string]bool, len(updates))
	for _, item := range updates {
		if index, ok := lastAssignment[item.key]; ok {
			_, prefix, suffix, _ := parseWSLConfigAssignment(lines[index])
			lines[index] = prefix + item.value + suffix
			updated[item.key] = true
		}
	}

	insertAt := sectionEnd
	for _, item := range updates {
		if !updated[item.key] {
			lines = append(lines, "")
			copy(lines[insertAt+1:], lines[insertAt:])
			lines[insertAt] = item.key + " = " + item.value
			insertAt++
			sectionEnd++
		}
	}

	// hostAddressLoopback is an experimental WSL setting, not a [wsl2]
	// setting. Keep it in its documented section so WSL does not silently
	// ignore the value after restart.
	experimentalStart, experimentalEnd := -1, len(lines)
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 2 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
			continue
		}
		section := strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
		if section == "experimental" && experimentalStart < 0 {
			experimentalStart = index
			continue
		}
		if experimentalStart >= 0 {
			experimentalEnd = index
			break
		}
	}
	if experimentalStart < 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "[experimental]")
		experimentalStart = len(lines) - 1
		experimentalEnd = len(lines)
	}
	hostAddressIndex := -1
	for index := experimentalStart + 1; index < experimentalEnd; index++ {
		key, _, _, ok := parseWSLConfigAssignment(lines[index])
		if ok && strings.EqualFold(key, "hostAddressLoopback") {
			hostAddressIndex = index
		}
	}
	if hostAddressIndex >= 0 {
		_, prefix, suffix, _ := parseWSLConfigAssignment(lines[hostAddressIndex])
		lines[hostAddressIndex] = prefix + settings.HostAddressLoopback + suffix
	} else {
		lines = append(lines, "")
		copy(lines[experimentalEnd+1:], lines[experimentalEnd:])
		lines[experimentalEnd] = "hostAddressLoopback = " + settings.HostAddressLoopback
	}
	result := strings.Join(lines, "\n")
	if hadTrailingNewline {
		result += "\n"
	}
	if newline != "\n" {
		result = strings.ReplaceAll(result, "\n", newline)
	}
	if hasBOM {
		result = "\ufeff" + result
	}
	return result, nil
}

// parseWSLConfigAssignment returns the original prefix and an inline comment
// suffix. It intentionally ignores comment lines and malformed assignments.
func parseWSLConfigAssignment(line string) (key, prefix, suffix string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
		return "", "", "", false
	}
	equal := strings.IndexByte(line, '=')
	if equal <= 0 {
		return "", "", "", false
	}
	key = strings.TrimSpace(line[:equal])
	if key == "" || strings.ContainsAny(key, "[]") {
		return "", "", "", false
	}
	value := line[equal+1:]
	commentAt := inlineWSLComment(value)
	content := value
	comment := ""
	if commentAt >= 0 {
		content = value[:commentAt]
		comment = value[commentAt:]
	}
	leading := content[:len(content)-len(strings.TrimLeft(content, " \t"))]
	trailing := content[len(strings.TrimRight(content, " \t")):]
	return key, line[:equal+1] + leading, trailing + comment, true
}

func inlineWSLComment(value string) int {
	for index := 1; index < len(value); index++ {
		if (value[index] == '#' || value[index] == ';') && (value[index-1] == ' ' || value[index-1] == '\t') {
			return index
		}
	}
	return -1
}

// UpdateWSLConfig performs a read/backup/atomic-replace transaction. A backup
// is created before the new file is published, and an existing backup is never
// overwritten: this makes repeated migrations recoverable even after a failed
// restart.
func UpdateWSLConfig(path string, settings WSLConfigSettings) (WSLConfigUpdate, error) {
	wslConfigMu.Lock()
	defer wslConfigMu.Unlock()
	return updateWSLConfig(path, settings)
}

func updateWSLConfig(path string, settings WSLConfigSettings) (WSLConfigUpdate, error) {
	if strings.TrimSpace(path) == "" {
		path = UserWSLConfigPath()
	}
	old, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		old = nil
	} else if err != nil {
		return WSLConfigUpdate{}, fmt.Errorf("ler %s: %w", path, err)
	}
	updated, err := UpdateWSLConfigText(string(old), settings)
	if err != nil {
		return WSLConfigUpdate{}, err
	}
	result := WSLConfigUpdate{Path: path, Changed: string(old) != updated}
	if !result.Changed {
		return result, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return WSLConfigUpdate{}, fmt.Errorf("criar diretório de %s: %w", path, err)
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	backup, err := writeUniqueWSLConfigBackup(path, old, mode)
	if err != nil {
		return WSLConfigUpdate{}, fmt.Errorf("criar backup de %s: %w", path, err)
	}
	result.BackupPath = backup
	temporary, err := os.CreateTemp(filepath.Dir(path), ".wslconfig-devlan-*.tmp")
	if err != nil {
		return WSLConfigUpdate{}, fmt.Errorf("criar temporário de %s: %w", path, err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return WSLConfigUpdate{}, err
	}
	if _, err := temporary.WriteString(updated); err != nil {
		_ = temporary.Close()
		return WSLConfigUpdate{}, fmt.Errorf("escrever temporário de %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return WSLConfigUpdate{}, err
	}
	if err := replaceFileAtomic(temporaryName, path); err != nil {
		return WSLConfigUpdate{}, fmt.Errorf("publicar %s atomicamente: %w", path, err)
	}
	return result, nil
}

// RestoreWSLConfig restores the exact bytes captured before a topology
// migration. It is intentionally separate from UpdateWSLConfig: rollback must
// not rewrite comments or create a second backup, and it must be atomic on the
// host just like the forward transaction.
func RestoreWSLConfig(path string, data []byte, existed bool) error {
	wslConfigMu.Lock()
	defer wslConfigMu.Unlock()
	return restoreWSLConfig(path, data, existed)
}

func restoreWSLConfig(path string, data []byte, existed bool) error {
	if strings.TrimSpace(path) == "" {
		path = UserWSLConfigPath()
	}
	if !existed {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".wslconfig-rollback-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFileAtomic(temporaryName, path)
}

func writeUniqueWSLConfigBackup(path string, data []byte, mode os.FileMode) (string, error) {
	base := path + ".devlan.bak"
	for attempt := 0; attempt < 100; attempt++ {
		candidate := base
		if attempt > 0 {
			candidate = fmt.Sprintf("%s.devlan-%s-%d.bak", path, time.Now().UTC().Format("20060102-150405.000000000"), attempt)
		}
		file, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, writeErr := file.Write(data); writeErr != nil {
			_ = file.Close()
			_ = os.Remove(candidate)
			return "", writeErr
		}
		if closeErr := file.Close(); closeErr != nil {
			_ = os.Remove(candidate)
			return "", closeErr
		}
		return candidate, nil
	}
	return "", errors.New("não foi possível reservar um backup exclusivo")
}

func WSLConfigHasMirroredNetworking(text string) bool {
	text = strings.TrimPrefix(text, "\ufeff")
	inWSL2 := false
	found := false
	mirrored := false
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if inWSL2 && found {
				return mirrored
			}
			inWSL2 = strings.EqualFold(strings.TrimSpace(trimmed[1:len(trimmed)-1]), "wsl2")
			found = false
			mirrored = false
			continue
		}
		if !inWSL2 {
			continue
		}
		key, _, _, ok := parseWSLConfigAssignment(raw)
		if !ok || !strings.EqualFold(key, "networkingMode") {
			continue
		}
		equal := strings.IndexByte(raw, '=')
		value := strings.TrimSpace(raw[equal+1:])
		if commentAt := inlineWSLComment(value); commentAt >= 0 {
			value = strings.TrimSpace(value[:commentAt])
		}
		found = true
		mirrored = strings.EqualFold(value, "mirrored")
	}
	return inWSL2 && found && mirrored
}
