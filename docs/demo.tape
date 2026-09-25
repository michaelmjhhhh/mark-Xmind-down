# Recreate the real terminal screenshot from the repository root:
# go build -o bin/xmind-md ./cmd/xmind-md
# vhs docs/demo.tape
# Requires VHS, ttyd, ffmpeg, and a Chromium-based browser.

Require ./bin/xmind-md

Set Shell "bash"
Set Width 1440
Set Height 560
Set FontSize 20
Set FontFamily "Menlo"
Set Theme "Catppuccin Mocha"
Set Padding 32
Set Margin 16
Set MarginFill "#11111b"
Set WindowBar Colorful
Set BorderRadius 12
Set TypingSpeed 0

Type "./bin/xmind-md assets --tui -d exports -f"
Enter
Wait+Screen /3 file\(s\) ready to export/
Enter
Wait+Screen /Finished 3 file\(s\)/
Sleep 1s
Screenshot docs/images/terminal.png
Sleep 500ms
Type "q"
