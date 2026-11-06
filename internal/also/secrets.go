package also

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// ScanConfigSecrets walks JSON, YAML and TOML files in a bundle and reports
// credential-looking keys with literal values. Values that reference the
// environment are allowed, because the secret is not stored in the package.
func ScanConfigSecrets(files map[string]*SnapshotFile) []string {
	var out []string
	for rel, f := range files {
		if f == nil || len(f.Data) > 4<<20 {
			continue
		}
		var root any
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".json":
			if err := json.Unmarshal(f.Data, &root); err != nil {
				continue
			}
		case ".yaml", ".yml":
			if err := yaml.Unmarshal(f.Data, &root); err != nil {
				continue
			}
		case ".toml":
			m := map[string]any{}
			if err := toml.Unmarshal(f.Data, &m); err != nil {
				continue
			}
			root = m
		default:
			continue
		}
		walkSecretKeys(root, nil, rel, &out)
	}
	sort.Strings(out)
	return uniqueStrings(out)
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
