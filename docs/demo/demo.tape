# docs/demo/README.md 의 순서로 실행한다 (dr 은 PATH 에, 예제는 /tmp/dr-demo 에).
Output docs/demo/demo.gif

Set Shell "bash"
Set FontSize 15
Set FontFamily "D2Coding"
Set Width 1280
Set Height 900
Set Padding 16
Set Theme "Catppuccin Mocha"
Set TypingSpeed 35ms

Hide
Type "export PS1='$ ' && cd /tmp/dr-demo && clear"
Enter
Show

Type "go test ./..."
Enter
Wait+Screen@60s /FAIL/
Sleep 1.5s

Type "dr"
Enter
Sleep 1.5s
Type "go test 가 실패해. 원인을 찾아서 고치고 테스트로 확인해 줘."
Sleep 500ms
Enter

Wait+Screen@300s /입력 .* 토큰/
Sleep 3s
Ctrl+D
Sleep 1s

Type "go test ./..."
Enter
Wait+Screen@60s /ok\s+wordcount/
Sleep 2.5s
