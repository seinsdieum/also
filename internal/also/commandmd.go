package also

import (
	"fmt"
	"regexp"
	"strings"
)

var qwenShellRe = regexp.MustCompile("!`([^`]*)`")
var qwenBraceShellRe = regexp.MustCompile(`!\{([^}]*)\}`)

// ConvertCommand translates the argument and shell placeholders that differ
// between Qwen Code and Claude/OpenCode command files.
func ConvertCommand(data []byte, from, to, name string) ([]byte, []string) {
	if from == to || (from != "qwen" && to != "qwen") {
		return data, nil
	}
	s := string(data)
	var warnings []string
	if to == "qwen" {
		if strings.Contains(s, "$ARGUMENTS") {
			s = strings.ReplaceAll(s, "$ARGUMENTS", "{{args}}")
		}
		if regexp.MustCompile(`\$\d`).MatchString(s) {
			warnings = append(warnings, fmt.Sprintf("command %s: positional $1..$9 arguments are not supported by Qwen; copied verbatim", name))
		}
		s = qwenShellRe.ReplaceAllString(s, "!{$1}")
	} else {
		s = strings.ReplaceAll(s, "{{args}}", "$ARGUMENTS")
		s = qwenBraceShellRe.ReplaceAllString(s, "!`$1`")
	}
	return []byte(s), warnings
}
