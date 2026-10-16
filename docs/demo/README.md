# 데모

`demo.gif` 는 `demo.tape` 를 [vhs](https://github.com/charmbracelet/vhs) 로 녹화한 결과입니다. 테스트가 실패하는 작은 Go 패키지(`project/`)에서 `dr` 을 실행합니다. 에이전트는 원인을 찾아 코드를 고친 뒤, 테스트를 다시 실행해서 결과를 확인합니다.

## 다시 만드는 방법

준비물은 아래와 같습니다.

- vhs, D2Coding 폰트 (`brew install vhs && brew install --cask font-d2coding`)
- 모델과 키가 등록된 `~/.dynamic_react/config.json`·`auth.json`

저장소 루트에서 아래 명령을 실행합니다.

```sh
go build -o /tmp/dr-bin/dr ./L3_ui/cmd/dr
rm -rf /tmp/dr-demo && cp -R docs/demo/project /tmp/dr-demo
PATH=/tmp/dr-bin:$PATH vhs docs/demo/demo.tape
```

## 주의

- 예제를 `/tmp/dr-demo` 로 복사해서 실행합니다. 저장소 안에서 실행하면 루트의 `go.work` 때문에 `go test` 가 실패합니다.
- `project/.dynamic_react/config.json` 은 이 예제 폴더에서만 모든 bash 명령을 허용합니다. 녹화 중에 승인 프롬프트가 뜨지 않게 하려는 설정입니다. 실제 프로젝트에는 이 설정을 복사하지 마십시오.
- `project/AGENTS.md` 는 에이전트가 `ls -la` 를 실행하지 않도록 지시합니다. `ls -la` 를 실행하면 화면에 로컬 사용자 이름이 나옵니다.
- 모델 응답은 실행할 때마다 다릅니다. 녹화한 뒤에는 마지막 프레임을 확인하십시오.
- `project/wordcount.go` 의 버그는 의도한 것입니다. 이 파일은 고치지 마십시오.
