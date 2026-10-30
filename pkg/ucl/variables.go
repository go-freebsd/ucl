package ucl

import "strings"

// VariableHandler is consulted for variables that are not registered with the
// parser. Returning false leaves the reference untouched, which is what
// libucl does for unknown variables.
type VariableHandler func(name string) (string, bool)

// expandVars replaces $VAR and ${VAR} references in s. Unknown variables are
// kept verbatim.
func expandVars(s string, vars map[string]string, handler VariableHandler) string {
	if !strings.ContainsRune(s, '$') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '$' || i+1 >= len(s) {
			b.WriteByte(s[i])
			i++
			continue
		}
		name, raw, next, ok := scanVarRef(s, i)
		if !ok {
			b.WriteByte(s[i])
			i++
			continue
		}
		if val, found := lookupVar(name, vars, handler); found {
			b.WriteString(val)
		} else {
			b.WriteString(raw)
		}
		i = next
	}
	return b.String()
}

// scanVarRef reads a variable reference starting at the dollar sign at s[i].
func scanVarRef(s string, i int) (name, raw string, next int, ok bool) {
	if s[i+1] == '{' {
		end := strings.IndexByte(s[i+2:], '}')
		if end < 0 {
			return "", "", 0, false
		}
		end += i + 2
		return s[i+2 : end], s[i : end+1], end + 1, true
	}
	j := i + 1
	for j < len(s) && isVarChar(s[j]) {
		j++
	}
	if j == i+1 {
		return "", "", 0, false
	}
	return s[i+1 : j], s[i:j], j, true
}

func isVarChar(c byte) bool {
	return c == '_' || isDigit(c) ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func lookupVar(name string, vars map[string]string, handler VariableHandler) (string, bool) {
	if v, ok := vars[name]; ok {
		return v, true
	}
	if handler != nil {
		return handler(name)
	}
	return "", false
}
