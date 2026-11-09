package also

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Cross-harness settings translation. The target adapter owns which keys it can
// express and reconciles the result against its own config; these helpers own
// the portable permission model shared by both directions.

// permRule is one parsed permission decision. Claude rules keep their native
// string because Claude-like targets understand the scope; OpenCode rules keep
// an action and a resource because their scope has to be resolved here.
type permRule struct {
	Claude   bool
	Tool     string // native Claude rule, e.g. "Bash(git status:*)"
	Action   string // OpenCode action, e.g. "bash"
	Resource string // "*" or a scoped resource
	Effect   string // allow, ask or deny
}

// scopeDecision is the outcome of the single fail-closed scope policy.
type scopeDecision int

const (
	scopeWrite scopeDecision = iota
	scopeDrop
	scopeUnknown
)

type ruleDecision struct {
	Decision  scopeDecision
	Keys      []string
	Label     string
	Broadened bool
}

// decideRule applies the one scope policy that both translators and both
// directions use. keepScope is true only when the source rule already uses the
// target's scoped syntax (Claude to Claude/Qwen). A flat target broadens scoped
// deny/ask and drops scoped allows, so a translated rule can never widen access.
func decideRule(keys []string, effect, resource, label string, keepScope bool) ruleDecision {
	if len(keys) == 0 {
		return ruleDecision{Decision: scopeUnknown, Label: label}
	}
	if keepScope {
		return ruleDecision{Decision: scopeWrite, Keys: keys, Label: label}
	}
	if resource != "*" && effect == "allow" {
		return ruleDecision{Decision: scopeDrop, Label: label}
	}
	return ruleDecision{Decision: scopeWrite, Keys: keys, Label: label, Broadened: resource != "*"}
}

// translateClaudeLikeSettings builds Claude Code or Qwen Code settings. Both
// share the Claude permission shape; only Claude Code has a theme setting.
func translateClaudeLikeSettings(settings map[string]any, target *Harness, supportsTheme bool) (map[string]any, []string) {
	out := map[string]any{}
	consumed := map[string]bool{"permission": true, "permissions": true}

	perms, warnings := translatePermissionsToClaude(settings, target)
	if len(perms) > 0 {
		out["permissions"] = perms
	}
	if supportsTheme {
		transferred, warns := transferTheme(settings, target.Display, out)
		consumed["theme"] = transferred
		warnings = append(warnings, warns...)
	}
	warnings = append(warnings, untranslatedSettingWarnings(settings, target.Display, consumed)...)
	return out, warnings
}

// translateOpenCodeSettings builds OpenCode v1 settings from a cross-harness
// source, using OpenCode's permission map and theme key.
func translateOpenCodeSettings(settings map[string]any, target *Harness) (map[string]any, []string) {
	out := map[string]any{}
	consumed := map[string]bool{"permission": true, "permissions": true}

	perms, warnings := translatePermissionsToOpenCode(settings, target)
	if len(perms) > 0 {
		out["permission"] = perms
	}
	transferred, warns := transferTheme(settings, target.Display, out)
	consumed["theme"] = transferred
	warnings = append(warnings, warns...)
	warnings = append(warnings, untranslatedSettingWarnings(settings, target.Display, consumed)...)
	return out, warnings
}

// transferTheme moves a string theme into out. It reports whether the key was
// handled so the caller does not warn about it twice. A theme name is a hint,
// not a guardrail, so a bad shape is dropped with a warning instead of an error.
func transferTheme(settings map[string]any, target string, out map[string]any) (bool, []string) {
	raw, ok := settings["theme"]
	if !ok {
		return false, nil
	}
	s, ok := raw.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return true, []string{fmt.Sprintf("setting %q has an unsupported shape and remains in the package", "theme")}
	}
	out["theme"] = s
	return true, []string{fmt.Sprintf("theme %q was translated for %s; the theme name may not exist there", s, target)}
}

func untranslatedSettingWarnings(settings map[string]any, target string, consumed map[string]bool) []string {
	var warnings []string
	for _, key := range sortedKeys(settings) {
		if consumed[key] {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("setting %q has no %s equivalent and remains in the package", key, target))
	}
	return warnings
}

func translateCodexSettings(settings map[string]any, target *Harness) (map[string]any, []string) {
	keys := sortedKeys(settings)
	return map[string]any{}, []string{fmt.Sprintf(
		"settings %s are preserved in the package: %s has no mergeable per-tool settings or permission model",
		strings.Join(keys, ", "), target.Display)}
}

// collectPermissionRules reads the three supported source shapes: Claude/Qwen
// permissions, OpenCode v1 permission and OpenCode v2 permissions. A source may
// carry v1 and v2 at once; both are merged. Malformed entries warn and are
// dropped so one bad rule cannot fail the install.
func collectPermissionRules(settings map[string]any, target string) ([]permRule, []string) {
	var rules []permRule
	var warnings []string

	if raw, ok := settings["permissions"]; ok {
		switch v := raw.(type) {
		case map[string]any:
			if !hasPermissionEffect(v) {
				warnings = append(warnings, "permission settings have an unsupported shape and were dropped; they remain in the package")
				break
			}
			for _, key := range sortedKeys(v) {
				if key == "allow" || key == "ask" || key == "deny" {
					continue
				}
				warnings = append(warnings, fmt.Sprintf("permission key %q has no %s equivalent and remains in the package", key, target))
			}
			for _, effect := range []string{"allow", "ask", "deny"} {
				rawList, exists := v[effect]
				if !exists {
					continue
				}
				list, ok := rawList.([]any)
				if !ok {
					warnings = append(warnings, fmt.Sprintf("permission %s list has an unsupported shape and was dropped; it remains in the package", effect))
					continue
				}
				for _, elem := range list {
					s, ok := elem.(string)
					if !ok || strings.TrimSpace(s) == "" {
						warnings = append(warnings, fmt.Sprintf("permission %s rule has an unsupported shape and was dropped; it remains in the package", effect))
						continue
					}
					rules = append(rules, permRule{Claude: true, Tool: strings.TrimSpace(s), Resource: "*", Effect: effect})
				}
			}
		case []any:
			for i, elem := range v {
				obj, ok := elem.(map[string]any)
				if !ok {
					warnings = append(warnings, fmt.Sprintf("permission entry %d has an unsupported shape and was dropped; it remains in the package", i+1))
					continue
				}
				action := strings.TrimSpace(stringField(obj, "action"))
				if action == "" {
					warnings = append(warnings, fmt.Sprintf("permission entry %d has no action and was dropped; it remains in the package", i+1))
					continue
				}
				resource := strings.TrimSpace(stringField(obj, "resource"))
				if resource == "" {
					resource = "*"
				}
				rules, warnings = addOpenCodeRule(rules, warnings, action, resource, stringField(obj, "effect"))
			}
		default:
			warnings = append(warnings, "permission settings have an unsupported shape and were dropped; they remain in the package")
		}
	}

	if raw, ok := settings["permission"]; ok {
		switch m := raw.(type) {
		case string:
			// OpenCode v1 also accepts a global default effect.
			rules, warnings = addOpenCodeRule(rules, warnings, "*", "*", m)
		case map[string]any:
			for _, action := range sortedKeys(m) {
				switch v := m[action].(type) {
				case string:
					rules, warnings = addOpenCodeRule(rules, warnings, action, "*", v)
				case map[string]any:
					for _, resource := range sortedKeys(v) {
						effect, ok := v[resource].(string)
						if !ok {
							warnings = append(warnings, fmt.Sprintf("permission rule %q has an unsupported effect and was dropped; it remains in the package", ruleLabel(action, resource)))
							continue
						}
						rules, warnings = addOpenCodeRule(rules, warnings, action, resource, effect)
					}
				default:
					warnings = append(warnings, fmt.Sprintf("permission %q has an unsupported shape and was dropped; it remains in the package", action))
				}
			}
		default:
			warnings = append(warnings, "permission map has an unsupported shape and was dropped; it remains in the package")
		}
	}
	return rules, warnings
}

func hasPermissionEffect(m map[string]any) bool {
	for _, key := range []string{"allow", "ask", "deny"} {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func addOpenCodeRule(rules []permRule, warnings []string, action, resource, effect string) ([]permRule, []string) {
	e, ok := normalizeEffect(effect)
	if !ok {
		warnings = append(warnings, fmt.Sprintf("permission rule %q has an unsupported effect %q and was dropped; it remains in the package", ruleLabel(action, resource), effect))
		return rules, warnings
	}
	return append(rules, permRule{Action: action, Resource: resource, Effect: e}), warnings
}

func ruleLabel(action, resource string) string {
	if resource == "" || resource == "*" {
		return action
	}
	return action + "(" + resource + ")"
}

func normalizeEffect(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "allow":
		return "allow", true
	case "ask":
		return "ask", true
	case "deny":
		return "deny", true
	}
	return "", false
}

func effectRank(effect string) int {
	switch effect {
	case "deny":
		return 3
	case "ask":
		return 2
	case "allow":
		return 1
	}
	return 0
}

// strongerEffect keeps deny over ask over allow. A later weaker rule must never
// turn an explicit restriction back into an allow.
func strongerEffect(current, next string) string {
	if effectRank(next) > effectRank(current) {
		return next
	}
	return current
}

func translatePermissionsToClaude(settings map[string]any, target *Harness) (map[string]any, []string) {
	rules, warnings := collectPermissionRules(settings, target.Display)
	if len(rules) == 0 {
		return nil, warnings
	}
	effects := map[string]string{}
	for _, r := range rules {
		if r.Claude {
			// Claude-native rules already use the target dialect. They still
			// go through the same scope decision, which keeps the scope.
			d := decideRule([]string{r.Tool}, r.Effect, r.Resource, r.Tool, true)
			if d.Decision == scopeWrite {
				effects[r.Tool] = strongerEffect(effects[r.Tool], r.Effect)
			}
			continue
		}
		tools := claudeToolsForAction(r.Action)
		label := ruleLabel(r.Action, r.Resource)
		if len(tools) > 0 && r.Resource != "*" {
			label = strings.Join(tools, "|") + "(" + r.Resource + ")"
		}
		d := decideRule(tools, r.Effect, r.Resource, label, false)
		switch d.Decision {
		case scopeUnknown:
			warnings = append(warnings, fmt.Sprintf("permission action %q has no %s equivalent and was dropped; it remains in the package", r.Action, target.Display))
			continue
		case scopeDrop:
			warnings = append(warnings, fmt.Sprintf("permission allow rule %q was dropped: %s cannot express a scoped allow safely; it remains in the package", d.Label, target.Display))
			continue
		}
		if d.Broadened {
			whole := "tool"
			if len(tools) > 1 {
				whole = "tools"
			}
			warnings = append(warnings, fmt.Sprintf("permission %s rule %q is scoped; %s cannot express the scope, so the translation covers the whole %s %s", r.Effect, d.Label, target.Display, strings.Join(tools, ", "), whole))
		}
		for _, tool := range tools {
			effects[tool] = strongerEffect(effects[tool], r.Effect)
		}
	}
	if len(effects) == 0 {
		return nil, warnings
	}
	out := map[string]any{"allow": []any{}, "ask": []any{}, "deny": []any{}}
	for _, tool := range sortedKeys(effects) {
		effect := effects[tool]
		out[effect] = append(out[effect].([]any), tool)
	}
	return out, warnings
}

func translatePermissionsToOpenCode(settings map[string]any, target *Harness) (map[string]any, []string) {
	rules, warnings := collectPermissionRules(settings, target.Display)
	if len(rules) == 0 {
		return nil, warnings
	}
	effects := map[string]string{}
	for _, r := range rules {
		effect, resource := r.Effect, r.Resource
		label := ruleLabel(r.Action, r.Resource)
		unknownKind, unknownName := "action", r.Action
		var keys []string
		if r.Claude {
			tool, res, ok := parseClaudeRule(r.Tool)
			if !ok {
				warnings = append(warnings, fmt.Sprintf("permission rule %q has an unsupported shape and was dropped; it remains in the package", r.Tool))
				continue
			}
			unknownKind, unknownName = "tool", tool
			label = r.Tool
			resource = res
			if action, known := actionForClaudeTool(tool); known {
				keys = []string{action}
			}
		} else if action, known := openCodeV1Action(r.Action); known {
			keys = []string{action}
		}
		d := decideRule(keys, effect, resource, label, false)
		switch d.Decision {
		case scopeUnknown:
			warnings = append(warnings, fmt.Sprintf("permission %s %q has no %s equivalent and was dropped; it remains in the package", unknownKind, unknownName, target.Display))
			continue
		case scopeDrop:
			warnings = append(warnings, fmt.Sprintf("permission allow rule %q was dropped: %s cannot express a scoped allow safely; it remains in the package", d.Label, target.Display))
			continue
		}
		if d.Broadened {
			warnings = append(warnings, fmt.Sprintf("permission %s rule %q is scoped; %s cannot express the scope, so the translation covers the whole %q action", effect, d.Label, target.Display, keys[0]))
		}
		effects[keys[0]] = strongerEffect(effects[keys[0]], effect)
	}
	if len(effects) == 0 {
		return nil, warnings
	}
	out := map[string]any{}
	for _, action := range sortedKeys(effects) {
		out[action] = effects[action]
	}
	return out, warnings
}

// reconcileClaudeSettings preserves permissions for the planner (arrays append)
// and re-owns an existing theme instead of overwriting a different target value.
func reconcileClaudeSettings(file string, settings map[string]any) (map[string]any, []string, error) {
	return reconcileTheme(file, settings)
}

// reconcileOpenCodeSettings adapts translated settings to the target's OpenCode
// config. It never replaces a scoped map and never appends to the V2 array.
func reconcileOpenCodeSettings(file string, settings map[string]any) (map[string]any, []string, error) {
	out, warnings, err := reconcileTheme(file, settings)
	if err != nil {
		return nil, nil, err
	}
	doc, ok, err := readTargetConfig(file)
	if err != nil || !ok {
		return out, warnings, err
	}
	if targetUsesPermissionsArray(doc) {
		out = copySettingsExcept(out, "permission", "permissions")
		warnings = append(warnings, fmt.Sprintf("target %s uses the V2 ordered permissions array; translated permission rules were not applied and remain in the package for manual review", file))
		return out, warnings, nil
	}
	raw, ok := out["permission"]
	if !ok {
		return out, warnings, nil
	}
	filtered, warns := reconcileOpenCodePermission(doc, raw, file)
	warnings = append(warnings, warns...)
	out = copySettingsExcept(out, "permission")
	if filtered != nil {
		out["permission"] = filtered
	}
	return out, warnings, nil
}

func targetUsesPermissionsArray(doc *JSONC) bool {
	raw, ok := doc.RawAt("permissions")
	if !ok {
		return false
	}
	var arr []any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return false
	}
	return len(arr) > 0
}

func reconcileOpenCodePermission(doc *JSONC, raw any, file string) (any, []string) {
	translated, ok := raw.(map[string]any)
	if !ok {
		return nil, []string{fmt.Sprintf("translated permission rules were not applied: %s has an unsupported permission shape", file)}
	}
	existingRaw, ok := doc.RawAt("permission")
	if !ok {
		return translated, nil
	}
	var existing map[string]any
	if err := json.Unmarshal(existingRaw, &existing); err != nil {
		global, isString := jsonStringValue(existingRaw)
		if !isString {
			return nil, []string{fmt.Sprintf("translated permission rules were not applied: %s has an unsupported permission shape", file)}
		}
		return reconcileOpenCodeGlobal(translated, global, file)
	}
	if existing == nil {
		return translated, nil
	}
	wildcard, hasWildcard := existing["*"]
	kept := map[string]any{}
	var warnings []string

	// Explicit actions first; the wildcard pass below strengthens overrides
	// without ever replacing a scoped map.
	for _, action := range sortedKeys(translated) {
		if action == "*" {
			continue
		}
		desired, ok := translated[action].(string)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("translated permission %q has an unsupported shape and remains in the package", action))
			continue
		}
		current, hasCurrent := effectivePermission(existing, action, wildcard, hasWildcard)
		if !hasCurrent {
			keepEffect(kept, action, desired)
			continue
		}
		switch cur := current.(type) {
		case string:
			existingEffect, okExisting := normalizeEffect(cur)
			desiredEffect, okDesired := normalizeEffect(desired)
			if !okExisting || !okDesired {
				warnings = append(warnings, fmt.Sprintf("permission %q was not applied: %s has an unsupported effect; the existing value was kept", action, file))
				continue
			}
			if effectRank(existingEffect) > effectRank(desiredEffect) {
				// Return the effective restriction so the planner re-owns it
				// after the previous install's edit is removed.
				keepEffect(kept, action, existingEffect)
				warnings = append(warnings, fmt.Sprintf("permission %q was not applied: %s already has the stronger %q effect; the existing value was kept", action, file, existingEffect))
				continue
			}
			keepEffect(kept, action, desiredEffect)
		case map[string]any:
			kept[action] = cur
			warnings = append(warnings, fmt.Sprintf("permission %q was not applied: %s keeps a scoped permission map; the existing policy was kept", action, file))
		default:
			warnings = append(warnings, fmt.Sprintf("permission %q was not applied: %s has an unsupported permission shape; the existing value was kept", action, file))
		}
	}

	if desiredRaw, ok := translated["*"]; ok {
		desired, isString := desiredRaw.(string)
		desiredEffect, validEffect := normalizeEffect(desired)
		if !isString || !validEffect {
			warnings = append(warnings, fmt.Sprintf("translated permission %q has an unsupported shape and remains in the package", "*"))
		} else {
			star, warns := reconcileWildcard(existing, wildcard, hasWildcard, desiredEffect, file)
			warnings = append(warnings, warns...)
			for action, value := range star {
				if action == "*" {
					kept["*"] = value
					continue
				}
				if effect, ok := value.(string); ok {
					keepEffect(kept, action, effect)
				} else {
					kept[action] = value
				}
			}
		}
	}
	return kept, warnings
}

// reconcileOpenCodeGlobal handles a target whose permission is one global
// string. A stronger translated "*" replaces the string. Stronger specific
// rules are written in a map that keeps the global default, because the global
// string cannot express per-action overrides.
func reconcileOpenCodeGlobal(translated map[string]any, global, file string) (any, []string) {
	globalEffect, ok := normalizeEffect(global)
	if !ok {
		return global, []string{fmt.Sprintf("permission %q was kept from %s because its effect is not supported", "*", file)}
	}
	result := globalEffect
	var warnings []string
	if raw, ok := translated["*"]; ok {
		desired, isString := raw.(string)
		desiredEffect, valid := normalizeEffect(desired)
		switch {
		case !isString || !valid:
			warnings = append(warnings, fmt.Sprintf("translated permission %q has an unsupported shape and remains in the package", "*"))
		case effectRank(desiredEffect) > effectRank(result):
			result = desiredEffect
		case effectRank(desiredEffect) < effectRank(result):
			warnings = append(warnings, fmt.Sprintf("permission %q was not applied: %s already has the stronger global %q effect; the existing value was kept", "*", file, result))
		}
	}
	overrides := map[string]any{}
	for _, action := range sortedKeys(translated) {
		if action == "*" {
			continue
		}
		desired, isString := translated[action].(string)
		desiredEffect, valid := normalizeEffect(desired)
		if !isString || !valid {
			warnings = append(warnings, fmt.Sprintf("translated permission %q has an unsupported shape and remains in the package", action))
			continue
		}
		if effectRank(desiredEffect) > effectRank(result) {
			overrides[action] = desiredEffect
		}
	}
	if len(overrides) == 0 {
		if result == globalEffect {
			// Keep the user's exact value (including casing) when nothing
			// stronger has to replace it.
			return global, warnings
		}
		return result, warnings
	}
	out := map[string]any{"*": global}
	if result != globalEffect {
		out["*"] = result
	}
	for action, effect := range overrides {
		out[action] = effect
	}
	return out, warnings
}

// jsonStringValue reports whether raw is a JSON string and returns it.
func jsonStringValue(raw json.RawMessage) (string, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(trimmed, "\"") {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

// reconcileWildcard compares a translated "*" with the target wildcard and
// strengthens specific actions that would otherwise keep a weaker effect. The
// wildcard is still written when only specific keys exist, because it covers
// every other action; specific keys continue to override it.
func reconcileWildcard(existing map[string]any, wildcard any, hasWildcard bool, desired string, file string) (map[string]any, []string) {
	kept := map[string]any{}
	var warnings []string
	var applied []string
	specifics := 0
	for _, action := range sortedKeys(existing) {
		if action == "*" {
			continue
		}
		specifics++
		switch cur := existing[action].(type) {
		case string:
			existingEffect, ok := normalizeEffect(cur)
			if !ok {
				warnings = append(warnings, fmt.Sprintf("permission %q was kept from %s because its effect is not supported", action, file))
				continue
			}
			if effectRank(desired) > effectRank(existingEffect) {
				kept[action] = desired
				applied = append(applied, action)
			}
		case map[string]any:
			kept[action] = cur
			warnings = append(warnings, fmt.Sprintf("permission %q was not applied: %s keeps a scoped permission map; the existing policy was kept", action, file))
		default:
			warnings = append(warnings, fmt.Sprintf("permission %q was kept from %s because its shape is not supported", action, file))
		}
	}

	if hasWildcard {
		switch cur := wildcard.(type) {
		case string:
			existingEffect, okExisting := normalizeEffect(cur)
			if !okExisting {
				warnings = append(warnings, fmt.Sprintf("permission %q was kept from %s because its effect is not supported", "*", file))
				return kept, warnings
			}
			if effectRank(existingEffect) > effectRank(desired) {
				kept["*"] = existingEffect
				warnings = append(warnings, fmt.Sprintf("permission %q was not applied: %s already has the stronger %q effect; the existing value was kept", "*", file, existingEffect))
				return kept, warnings
			}
			kept["*"] = desired
		case map[string]any:
			kept["*"] = cur
			warnings = append(warnings, fmt.Sprintf("permission %q was not applied: %s keeps a scoped permission map; the existing policy was kept", "*", file))
		default:
			warnings = append(warnings, fmt.Sprintf("permission %q was kept from %s because its shape is not supported", "*", file))
		}
		return kept, warnings
	}

	if specifics == 0 {
		kept["*"] = desired
		return kept, warnings
	}
	kept["*"] = desired
	if len(applied) > 0 {
		warnings = append(warnings, fmt.Sprintf("permission %q was written as the global rule in %s; stronger specific rules were applied to %s and the existing specific keys were kept", "*", file, strings.Join(applied, ", ")))
	} else {
		warnings = append(warnings, fmt.Sprintf("permission %q was written as the global rule in %s; the existing specific keys were kept", "*", file))
	}
	return kept, warnings
}

// keepEffect merges a flat effect without weakening a restriction and without
// replacing a scoped map with a broader flat rule.
func keepEffect(kept map[string]any, action, effect string) {
	if _, ok := kept[action].(map[string]any); ok {
		return
	}
	if cur, ok := kept[action].(string); ok {
		kept[action] = strongerEffect(cur, effect)
		return
	}
	kept[action] = effect
}

// effectivePermission resolves the value a translated action competes with.
// A specific key wins over the wildcard; otherwise the wildcard applies.
func effectivePermission(existing map[string]any, action string, wildcard any, hasWildcard bool) (any, bool) {
	if action != "*" {
		if value, ok := existing[action]; ok {
			return value, true
		}
	}
	if hasWildcard {
		return wildcard, true
	}
	return nil, false
}

func reconcileTheme(file string, settings map[string]any) (map[string]any, []string, error) {
	if _, ok := settings["theme"]; !ok {
		return settings, nil, nil
	}
	doc, ok, err := readTargetConfig(file)
	if err != nil || !ok {
		return settings, nil, err
	}
	existing, ok := doc.RawAt("theme")
	if !ok {
		return settings, nil, nil
	}
	want, err := json.Marshal(settings["theme"])
	if err != nil {
		return nil, nil, err
	}
	if jsonEqual(existing, want) {
		return settings, nil, nil
	}
	var current any
	if err := json.Unmarshal(existing, &current); err != nil {
		return nil, nil, err
	}
	out := copySettingsExcept(settings, "theme")
	out["theme"] = current
	return out, []string{fmt.Sprintf("setting %q is already configured differently in %s; kept the existing target value", "theme", file)}, nil
}

func readTargetConfig(file string) (*JSONC, bool, error) {
	if file == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	doc, err := ParseJSONC(data)
	if err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", file, err)
	}
	return doc, true, nil
}

func copySettingsExcept(settings map[string]any, drop ...string) map[string]any {
	skip := map[string]bool{}
	for _, key := range drop {
		skip[key] = true
	}
	out := map[string]any{}
	for key, value := range settings {
		if !skip[key] {
			out[key] = value
		}
	}
	return out
}

// parseClaudeRule splits a Claude tool rule such as "Bash(git status:*)" into
// the tool and its resource scope. An unscoped rule has resource "*".
func parseClaudeRule(rule string) (tool, resource string, ok bool) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return "", "", false
	}
	open := strings.IndexByte(rule, '(')
	if open < 0 {
		return rule, "*", true
	}
	close := strings.LastIndexByte(rule, ')')
	if close <= open || strings.TrimSpace(rule[close+1:]) != "" {
		return "", "", false
	}
	tool = strings.TrimSpace(rule[:open])
	resource = strings.TrimSpace(rule[open+1 : close])
	if tool == "" {
		return "", "", false
	}
	if resource == "" {
		resource = "*"
	}
	return tool, resource, true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
