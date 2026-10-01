package terminal

import (
	"errors"
	"os"
	"sync"

	"golang.org/x/term"
)

var ErrNotTerminal = errors.New("terminal: not a terminal")

// IsTerminal 은 fd 가 TTY 인지다 (TM-FR-01).
func IsTerminal(fd int) bool { return term.IsTerminal(fd) }

// Open 은 f 를 raw mode 로 바꾼다. restore 는 원래 모드로 되돌리고 여러 번 불러도 된다 (TM-FR-01).
func Open(f *os.File) (func() error, func() int, error) {
	fd := int(f.Fd())
	if !term.IsTerminal(fd) {
		return nil, nil, ErrNotTerminal
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, nil, err
	}
	var once sync.Once
	var rerr error
	restore := func() error {
		once.Do(func() { rerr = term.Restore(fd, old) })
		return rerr
	}
	width := func() int {
		w, _, err := term.GetSize(fd)
		if err != nil {
			return 0
		}
		return w
	}
	return restore, width, nil
}
