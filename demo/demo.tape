# optimus demo — render with: demo/record.sh
Output demo/optimus.mp4
Output demo/optimus.gif

Require python3
Require tmux

Set Shell bash
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 1600
Set Height 920
Set Padding 18
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms
Set CursorBlink false
Set Framerate 30

Hide
Type "source demo/setup.sh && clear"
Enter
Sleep 2.5s
Show

# --- the dashboard: every agent at a glance
Type "optimus"
Sleep 400ms
Enter
Sleep 3.5s
Down
Sleep 1.8s
Down
Sleep 1.8s
Down
Sleep 1.5s
Up
Up
Up
Sleep 1.2s

# --- jump into an agent, and back with Alt-q
Enter
Sleep 4.5s
Alt+q
Sleep 2s

# --- broadcast one prompt to every agent
Type "b"
Sleep 800ms
Type "run the test suite and summarize any failures"
Sleep 600ms
Enter
Sleep 2.5s
Down
Sleep 2.5s
Down
Sleep 2.5s
Up
Up
Sleep 1.5s

# --- every session of every agent
Type "2"
Sleep 2.5s
Type "/"
Sleep 300ms
Type "rate"
Sleep 900ms
Enter
Sleep 500ms
Enter
Sleep 2.5s
Up@12
Sleep 1.5s

# --- hand the context of a Claude session to Codex
Type "h"
Sleep 1.8s
Type "codex"
Sleep 900ms
Enter
Sleep 9s
Alt+q
Sleep 2s

# --- projects and spend
Type "3"
Sleep 3s
Type "4"
Sleep 6s
Type "?"
Sleep 3.5s
Escape
Sleep 500ms
Type "q"
Sleep 800ms

# --- all of it is scriptable too
Type "optimus usage --by agent --since 14d"
Sleep 400ms
Enter
Sleep 3s
Type "optimus limits"
Sleep 400ms
Enter
Sleep 4s
