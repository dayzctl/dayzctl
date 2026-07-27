package utils

import "strings"

// FormatModName adds @ prefix and normalizes (lowercase, underscores)
func FormatModName(name string) string {
	name = strings.TrimPrefix(name, "@")
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	return "@" + name
}
