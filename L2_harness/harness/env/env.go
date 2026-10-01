// Package env 는 harness 가 실제 입출력을 맡기는 interface 다 (HN-C1, R14).
// OS 구현은 L3_ui/osenv, 테스트용 가짜는 envtest 에 있다.
package env

import (
	"context"
	"io"
	"time"
)

type FileInfo struct {
	Name  string
	IsDir bool
	Size  int64
	// Symlink 은 ListDir 항목이 심볼릭 링크라는 뜻이다. IsDir·Size 는 대상 기준이다.
	Symlink bool
}

// FileSystem 의 경로는 절대 경로다. 없는 파일은 errors.Is(err, fs.ErrNotExist) 인 에러다 (HN-FR-01, 02).
type FileSystem interface {
	ReadFile(ctx context.Context, path string) ([]byte, error)
	// WriteFile 은 생성·덮어쓰기이고 상위 디렉터리를 만든다.
	WriteFile(ctx context.Context, path string, data []byte) error
	// AppendFile 은 없으면 만들고 상위 디렉터리를 만든다. 호출 하나는 한 번의 write 다 (HN-FR-03).
	AppendFile(ctx context.Context, path string, data []byte) error
	Stat(ctx context.Context, path string) (FileInfo, error)
	// ListDir 은 이름순이다.
	ListDir(ctx context.Context, path string) ([]FileInfo, error)
	CreateTemp(ctx context.Context, pattern string) (path string, w io.WriteCloser, err error)
}

type ExecRequest struct {
	Command string
	Dir     string
	Timeout time.Duration // 0 = 없음
}

type ExecResult struct {
	ExitCode int
	TimedOut bool
}

// Shell 은 Command 를 셸로 실행하고 stdout·stderr 를 합쳐 out 에 흘린다.
// ctx 취소·Timeout 시 자식 프로세스 전체를 끝내고 반환한다. Timeout 은 에러가 아니라 TimedOut 이다 (HN-FR-03).
type Shell interface {
	Exec(ctx context.Context, req ExecRequest, out io.Writer) (ExecResult, error)
}

type Env interface {
	FileSystem
	Shell
	Cwd() string
	Home() string
}
