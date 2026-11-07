package also

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// ScanConfigSecrets walks every file in a bundle and reports credential-looking
// keys with literal values. Structured files are parsed; plain text and files
// with comments are scanned as text. Environment references are allowed.
func ScanConfigSecrets(files map[string]*SnapshotFile) []string {
	var out []string
	for rel, f := range files {
		if f == nil || len(f.Data) > 4<<20 {
			continue
		}
		var root any
		parsed := true
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".json":
			if err := json.Unmarshal(f.Data, &root); err != nil {
				parsed = false
			}
		case ".yaml", ".yml":
			if err := yaml.Unmarshal(f.Data, &root); err != nil {
				parsed = false
			}
		case ".toml":
			m := map[string]any{}
			if err := toml.Unmarshal(f.Data, &m); err != nil {
				parsed = false
			} else {
				root = m
			}
		default:
			parsed = false
		}
		if parsed {
			walkSecretKeys(root, nil, rel, &out)
		} else {
			out = append(out, scanTextSecrets(rel, f.Data)...)
		}
	}
	sort.Strings(out)
	return UniqueStrings(out)
}

var textSecretRe = regexp.MustCompile(`(?i)(token|secret|passw|api[-_]?key|authorization|credential|private[-_]?key)["']?\s*[:=]\s*["']?([^\s"',;]+)`)
var placeholderRe = regexp.MustCompile(`(?i)^(your|example|placeholder|changeme|xxx|<|\.\.\.)`)

func scanTextSecrets(rel string, data []byte) []string {
	var out []string
	for _, m := range textSecretRe.FindAllSubmatch(data, -1) {
		v := string(m[2])
		if len(v) < 8 || isEnvRef(v) || placeholderRe.MatchString(v) {
			continue
		}
		out = append(out, rel+":"+string(m[1]))
	}
	return out
}

func walkSecretKeys(v any, path []string, file string, out *[]string) {
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			if s, ok := child.(string); ok {
				if s != "" && secretKeyRe.MatchString(k) && !isEnvRef(s) {
					p := append(append([]string{}, path...), k)
					*out = append(*out, fmt.Sprintf("%s:%s", file, strings.Join(p, ".")))
				}
				continue
			}
			walkSecretKeys(child, append(path, k), file, out)
		}
	case map[any]any:
		for k, child := range val {
			walkSecretKeys(child, append(path, fmt.Sprint(k)), file, out)
		}
	case []any:
		for _, child := range val {
			walkSecretKeys(child, path, file, out)
		}
	}
}
