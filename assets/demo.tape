# Demo recording for the README, driven by VHS (https://github.com/charmbracelet/vhs).
#
#   brew install vhs
#   vhs assets/demo.tape
#
# Checked in so the demo can be regenerated when the UI changes, rather than
# being a hand-recorded artifact nobody can reproduce. It drives the real binary
# built from the working tree against a throwaway data file — see demo-seed.sh.

Output assets/demo.gif

Set Shell bash
Set FontSize 15
Set Width 1180
Set Height 700
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms

# Build and seed off camera.
Hide
Type "source assets/demo-seed.sh"
Enter
Sleep 8s
Type "clear"
Enter
Sleep 1s
Show

# Capture from the shell, the way most tasks actually arrive.
Type "ike add 'Post the v0.2 announcement' -q 2"
Sleep 700ms
Enter
Sleep 1800ms

# Then open the matrix.
Type "ike"
Sleep 500ms
Enter
Sleep 2500ms

# Move around quadrant 1.
Type "j"
Sleep 900ms
Type "k"
Sleep 1200ms

# The task carries a plan — open it.
Type "p"
Sleep 3000ms
Escape
Sleep 1200ms

# Capture straight into the matrix.
Type "a"
Sleep 600ms
Type "Reply to the packaging thread"
Sleep 700ms
Enter
Sleep 1800ms

# Reclassify it: not important, just urgent.
Type "m"
Sleep 900ms
Type "3"
Sleep 2000ms

# Undo history spans every frontend and survives restarts, and says what it
# undid rather than just moving something back.
Type "u"
Sleep 3000ms

# Ends on the matrix rather than back at a bare prompt, so the loop reads as a
# tool rather than as a session that finished.
Sleep 500ms
