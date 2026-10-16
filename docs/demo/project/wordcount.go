package wordcount

import "strings"

// Count 는 s 의 단어 수를 센다.
func Count(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, " "))
}
