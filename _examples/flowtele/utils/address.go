package utils

import "regexp"

func CleanStringForFS(s string) string {
	return regexp.MustCompile("[:.]").ReplaceAllString(s, "")
}
