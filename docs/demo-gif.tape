# Record from the repository root:
# go build -o bin/xmind-md ./cmd/xmind-md
# env -u NO_COLOR COLORTERM=truecolor vhs docs/demo-gif.tape
# Requires VHS, ttyd, ffmpeg, and a Chromium-based browser.
# Sample copies and exports stay in an isolated temporary directory.

Output docs/images/demo.gif
Require ./bin/xmind-md

Set Shell "bash"
Set Width 1440
Set Height 680
Set FontSize 20
Set FontFamily "Menlo"
Set Theme "Catppuccin Mocha"
Set Padding 32
Set Margin 16
Set MarginFill "#11111b"
Set WindowBar Colorful
Set BorderRadius 12
Set Framerate 15
Set TypingSpeed 45ms

Hide
Type@0ms `export PATH="$PWD/bin:$PATH"; demo_dir=$(mktemp -d /tmp/xmind-demo.XXXXXX); cp assets/*.xmind "$demo_dir/"; cd "$demo_dir"; export PS1='$ '`
Enter
Sleep 500ms
Ctrl+L
Show

Type "xmind-md --tui . -d exports"
Sleep 500ms
Enter
Wait+Screen /XMind files/
Sleep 1500ms
Down
Sleep 500ms
Space
Sleep 1s
Down
Sleep 500ms
Type "a"
Sleep 1500ms
Type "?"
Wait+Screen /Keyboard shortcuts/
Sleep 3s
Type "?"
Wait+Screen /XMind files/
Sleep 1s
Type "e"
Wait+Screen /Finished 3 file\(s\)/
Sleep 4s

Hide
Type "q"
Sleep 500ms
Type@0ms `cd /; rm -rf -- "$demo_dir"`
Enter
Sleep 500ms
