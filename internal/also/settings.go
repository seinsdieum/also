package also

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

// settingsExtractKeys is the union of same-harness and cross-harness keys.
func settingsExtractKeys(h *Harness) []string {
	return UniqueStrings(append(append([]string{}, h.Settings.Keys...), h.Settings.CrossKeys...))
}

// ExtractBundleSettings reads the harness-native settings keys that can be
// merged safely on same-harness installs plus the keys reserved for
// cross-harness translation (permissions, statusLine, theme, ...).
func ExtractBundleSettings(h *Harness, files map[string]*SnapshotFile, roots []string) (map[string]any, []string, error) {
	out := map[string]any{}
	keys := settingsExtractKeys(h)
	if len(keys) == 0 {
		return out, nil, nil
	}
	if h.Settings.Extract != nil {
		return h.Settings.Extract(keys, h.Settings.Configs, files, roots)
	}
	data, _, found := findBundleConfig(files, roots, h.Settings.Configs)
	if !found {
		return out, nil, nil
	}
	doc, err := ParseJSONC(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse settings: %w", err)
	}
	var warnings []string
	sort.Strings(keys)
	for _, key := range keys {
		raw, ok := doc.RawAt(key)
		if !ok {
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			warnings = append(warnings, fmt.Sprintf("settings key %q has an unsupported shape and was preserved only", key))
			continue
		}
		out[key] = v
	}
	return out, warnings, nil
}

// SettingsTargetFile is the config file that holds SettingsKeys for a target.
func SettingsTargetFile(t Target) string {
	return t.Harness.SettingsTarget(t)
}

// BundleLayerPrefix is reserved for native harness layers preserved by adopt.
const BundleLayerPrefix = ".also-layers"

func layerName(p string) string {
	prefix := BundleLayerPrefix + "/"
	if !strings.HasPrefix(p, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(p, prefix)
	if i := strings.IndexByte(rest, '/'); i > 0 {
		return rest[:i]
	}
	return ""
}

// splitLayers separates reserved .also-layers/<harness>/... files from a package.
func splitLayers(files map[string]*SnapshotFile) map[string]map[string]*SnapshotFile {
	out := map[string]map[string]*SnapshotFile{}
	prefix := BundleLayerPrefix + "/"
	for rel, f := range files {
		name := layerName(rel)
		if name == "" {
			continue
		}
		rest := strings.TrimPrefix(rel, prefix+name+"/")
		if rest == "" || rest == rel {
			continue
		}
		if out[name] == nil {
			out[name] = map[string]*SnapshotFile{}
		}
		if err := SafeRel(rest); err == nil {
			out[name][rest] = f
		}
	}
	return out
}

func layerPath(harness, rel string) string {
	return path.Join(BundleLayerPrefix, harness, rel)
}

func loadLayerManifest(files map[string]*SnapshotFile) map[string]map[string]string {
	f := files[BundleLayerPrefix+"/manifest.json"]
	if f == nil {
		return map[string]map[string]string{}
	}
	var m struct {
		Layers map[string]map[string]string `json:"layers"`
	}
	if json.Unmarshal(f.Data, &m) != nil {
		return map[string]map[string]string{}
	}
	if m.Layers == nil {
		m.Layers = map[string]map[string]string{}
	}
	return m.Layers
}
