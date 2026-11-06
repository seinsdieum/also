package also

import (
	"fmt"
	"regexp"
	"strings"
)

var qwenShellRe = regexp.MustCompile("!`([^`]*)`")
var qwenBraceShellRe = regexp.MustCompile(`!\{([^}]*)\}`)

// Command conversion is a registry of dialect pairs. A new dialect registers
// its pair in init instead of editing ConvertCommand.
type CommandConverter func(data []byte, from, to, name string) ([]byte, []string)

var commandFormats = map[string]bool{
	CommandClaude: true,
	CommandQwen:   true,
}

var commandConverters = map[string]CommandConverter{
	CommandClaude + ">" + CommandQwen: commandToQwen,
	CommandQwen + ">" + CommandClaude: commandFromQwen,
}

func RegisterCommandFormat(name string) {
	if strings.TrimSpace(name) == "" {
		panic("command format needs a name")
	}
	commandFormats[name] = true
}

func RegisterCommandConverter(from, to string, fn CommandConverter) {
	if fn == nil || from == "" || to == "" {
		panic("command converter needs from, to and implementation")
	}
	RegisterCommandFormat(from)
	RegisterCommandFormat(to)
	commandConverters[from+">"+to] = fn
}

func commandFormatKnown(f string) bool {
	return commandFormats[f]
}

// ConvertCommand translates the argument and shell placeholders that differ
// between command dialects. The adapter declares each harness's dialect.
func ConvertCommand(data []byte, from, to, name string) ([]byte, []string) {
	fromFormat, toFormat := commandFormatOf(from), commandFormatOf(to)
	if fromFormat == "" || toFormat == "" || fromFormat == toFormat {
		return data, nil
	}
	if fn, ok := commandConverters[fromFormat+">"+toFormat]; ok {
		return fn(data, fromFormat, toFormat, name)
	}
	return data, []string{fmt.Sprintf("command %s: no converter from %s to %s; copied verbatim", name, fromFormat, toFormat)}
}

func commandFormatOf(harness string) string {
	if h, ok := ResolveHarness(harness); ok {
		return h.Commands.Format
	}
	return ""
}

func commandToQwen(data []byte, _, _, name string) ([]byte, []string) {
	s := string(data)
	var warnings []string
	if strings.Contains(s, "$ARGUMENTS") {
		s = strings.ReplaceAll(s, "$ARGUMENTS", "{{args}}")
	}
	if regexp.MustCompile(`\$\d`).MatchString(s) {
		warnings = append(warnings, fmt.Sprintf("command %s: positional $1..$9 arguments are not supported by Qwen; copied verbatim", name))
	}
	s = qwenShellRe.ReplaceAllString(s, "!{$1}")
	return []byte(s), warnings
}

func commandFromQwen(data []byte, _, _, _ string) ([]byte, []string) {
	s := strings.ReplaceAll(string(data), "{{args}}", "$ARGUMENTS")
	s = qwenBraceShellRe.ReplaceAllString(s, "!`$1`")
	return []byte(s), nil
}
