# Command Hints Spec

## v1.1.0 - 2026-11-05

- User approved the four follow-up optimizations: bounded discovery of host-only executable names, safe path suggestions, platform-aware syntax, and Chinese descriptions with better candidate ranking.
- Reuse the opt-in host-query switch; preserve new-install-only Off and existing preferences. No arbitrary help/generator execution, persistent host inventory, or terminal Tab takeover.

## v1.0.1 - 2026-11-05

- Clarify that the opt-in command probe verifies static candidates but does not inventory unknown host commands.
- Require current Bash/Zsh line-clear binding evidence and a fresh output drain before each physical preview write.

## v1.0.0 - 2026-11-05

- Initial scope for issue #510: broad offline command hints, opt-in host detection, and guarded live preview. The user confirmed that only new installations default to Off; the marker is device-local so a new computer cannot overwrite existing synced preferences.
