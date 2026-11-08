# The README's terminal-colour shot (assets/readme-settings-colors.png): the
# Settings page open at Terminal > Terminal colors, in the light appearance.
#
#   go build -tags vivahead -o /tmp/motty-head ./cmd/motty
#   MOTTY_LANG=en python3 tools/headless_drive.py "$(mktemp -d)" tools/e2e/readme_settings_colors.txt
#
# Light is chosen in Settings rather than with the appearance line: Motty opens
# pinned to dark and ignores the system appearance until the reader asks for
# another look.
#
# The section is scrolled into view because Terminal colors sits below the font
# and padding rows, off the first screen of the Terminal page. The scroll amount
# is the one that brings the colour swatches to the upper half; a larger scroll
# leaves only the swatches and no heading to say what they are.
resize 1560 975
settle 600
click "Settings"
settle 900
click "Light"
settle 600
click "Terminal"
settle 900
scroll 780 500 0 1500
settle 900
shot /tmp/readme-settings-colors.png
layout
