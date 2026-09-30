package adapters

import (
	"bufio"
	"io"
	"strings"
)

// readSSE 는 Server-Sent Events 를 읽어 이벤트마다 fn 을 부른다. fn 이 false 면 멈춘다 (AD-C3 의 자체 파서).
func readSSE(r io.Reader, fn func(event, data string) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var event string
	var data []string
	flush := func() bool {
		if len(data) == 0 {
			event = ""
			return true
		}
		ok := fn(event, strings.Join(data, "\n"))
		event, data = "", nil
		return ok
	}
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		switch {
		case line == "":
			if !flush() {
				return nil
			}
		case strings.HasPrefix(line, ":"):
		default:
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch field {
			case "event":
				event = value
			case "data":
				data = append(data, value)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	flush()
	return nil
}
