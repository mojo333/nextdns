package host

import (
	"errors"
	"fmt"
)

// logGrepPattern builds the grep BRE pattern used to filter service log
// lines. The service name is restricted to a safe charset so callers can
// never inject pattern syntax into the grep command.
func logGrepPattern(name string) (string, error) {
	if name == "" {
		return "", errors.New("empty service name")
	}
	for _, r := range name {
		isLower := r >= 'a' && r <= 'z'
		isUpper := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		if !isLower && !isUpper && !isDigit && r != '_' && r != '-' {
			return "", fmt.Errorf("invalid service name: %q", name)
		}
	}
	return fmt.Sprintf(` %s\(:\|\[\)`, name), nil
}
