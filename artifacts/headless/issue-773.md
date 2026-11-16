# Qoder CLI Provider Preset

Issue: https://github.com/t8y2/motty/issues/773
Captured: 2026-11-15.

These PNGs render the real AI settings page through the Viva headless build
(`vivahead` + `VIVA_HEADLESS=1`). No application window was opened. The Qoder
CLI path is a fixture install under `/tmp`; installed account data and live
model requests were not used.

| File | State |
| --- | --- |
| [773-qoder-preset.png](773-qoder-preset.png) | The add-provider preset list with Qoder CLI beside the other local CLI providers |
| [773-qoder-form.png](773-qoder-form.png) | Qoder CLI selected, with its automatic executable path discovered |

Regenerate from the repository root with a headless build and the line protocol:

```sh
go build -tags vivahead,dev -o /tmp/motty-head ./cmd/motty
cat > /tmp/qoder-shot.txt <<'EOF'
# lang: zh-CN
click "设置"
wait 100
click "AI 助手"
wait 200
click "添加服务商"
wait 200
click id:ai-settings/ai-set-form-preset
wait 300
shot artifacts/headless/773-qoder-preset.png
click "Qoder CLI"
wait 400
shot artifacts/headless/773-qoder-form.png
EOF
MOTTY_HEAD=/tmp/motty-head python3 tools/headless_drive.py /tmp/motty-home /tmp/qoder-shot.txt
```

The fixture `qoder` executable was placed on `PATH` for the automatic lookup.
The form and command construction are also covered by `internal/ai/cli` tests;
a real Qoder CLI 1.1.89 probe reached the normal sign-in-required path with the
scoped arguments.
