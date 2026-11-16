package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"motty/internal/completion"
)

func TestLocaleGenerationRequiresCurrentSourceContent(t *testing.T) {
	catalog, err := completion.NewCatalog("test", "v1", "", "MIT", []*completion.Node{{Token: "tool", Desc: "Delete a branch", Flags: []completion.Flag{{Long: "mode", Desc: "Select mode", Value: true, Values: []completion.Value{{Text: "fast", Desc: "Skip slow checks"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	sources, err := descriptionSources(catalog)
	if err != nil {
		t.Fatal(err)
	}
	translations := map[string]string{"Delete a branch": "删除分支", "Select mode": "选择模式", "Skip slow checks": "跳过较慢的检查", "Old source": "旧说明"}
	data, err := generate(sources, translations)
	if err != nil {
		t.Fatal(err)
	}
	table, err := completion.DecodeDescriptions(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := table.Lookup("Delete a branch"); got != "删除分支" {
		t.Fatalf("translated description = %q", got)
	}
	if got := table.Lookup("Old source"); got != "Old source" {
		t.Fatalf("stale source was included: %q", got)
	}
	sources["Delete all branches"] = "tool delete"
	if _, err := generate(sources, translations); err == nil {
		t.Fatal("changed English content silently reused the old translation")
	}
	var missing bytes.Buffer
	if err := writeMissing(&missing, sources, translations); err != nil {
		t.Fatal(err)
	}
	if got := missing.String(); got != "english\tzh-CN\tcontext\nDelete all branches\t\ttool delete\n" {
		t.Fatalf("incremental export = %q", got)
	}
}

func TestLocaleGenerationIncludesShadowedFlagDescriptions(t *testing.T) {
	catalog, err := completion.NewCatalog("test", "v1", strings.Repeat("a", 40), "MIT", []*completion.Node{{
		Token:      "tool",
		Flags:      []completion.Flag{{Long: "mode", Desc: "Local mode"}},
		Persistent: []completion.Flag{{Long: "mode", Desc: "Global mode"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err = completion.Decode(catalog.Encode())
	if err != nil {
		t.Fatal(err)
	}
	sources, err := descriptionSources(catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, english := range []string{"Local mode", "Global mode"} {
		if _, exists := sources[english]; !exists {
			t.Fatalf("shadowed description %q is missing", english)
		}
	}
}

func TestBundledDescriptionsRegenerate(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := run([]string{
		"-catalog", filepath.Join(root, "internal/completion/data/catalog.bin"),
		"-translations", "translations.zh-CN.tsv",
		"-out", filepath.Join(root, "internal/completion/data/descriptions.zh-CN.gz"),
		"-check",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTranslationSourceRejectsLostTechnicalLiterals(t *testing.T) {
	for _, pair := range [][2]string{
		{"Use --force to delete <NAME>", "使用 --force 删除 <NAME>"},
		{"Read $HOME and ${PATH}", "读取 $HOME 和 ${PATH}"},
		{"See `git help` and https://example.org/help", "参见 `git help` 和 https://example.org/help"},
		{"Set -H to use \"http/1.1\"", "设置 -H 以使用 \"http/1.1\""},
	} {
		if err := validateTranslation(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
		if err := validateTranslation(pair[0], "已翻译的说明"); err == nil {
			t.Fatalf("lost literals in %q were accepted", pair[0])
		}
	}
	if _, err := readTranslations(strings.NewReader("english\tzh-CN\tcontext\nDelete\t删除\ttool\nDelete\t保留\ttool\n")); err == nil {
		t.Fatal("conflicting duplicate source was accepted")
	}
	if _, err := readTranslations(strings.NewReader("english\tzh-CN\tcontext\nDelete\t\ttool\n")); err != nil {
		t.Fatalf("missing export cannot be read back: %v", err)
	}
	if err := validateTranslation("Do NOT cache failed requests", "不要缓存失败的请求"); err != nil {
		t.Fatalf("emphasized prose was treated as a parameter: %v", err)
	}
	if err := validateTranslation("NEVER fail the build, regardless of project result", "无论项目结果如何，都不让构建失败"); err != nil {
		t.Fatalf("emphasized negation was treated as a parameter: %v", err)
	}
	if err := validateTranslation("Add space after matched text. Defaults to auto, when used with --multiple.", "匹配文本后追加空格，默认 auto，与 --multiple 配合使用"); err != nil {
		t.Fatalf("ordinary prose was treated as enum values: %v", err)
	}
	if err := validateTranslation("Use -v for info, -vv for debug; show ID and UUID", "使用 -v 显示信息，使用 -vv 调试；显示 ID 和 UUID"); err != nil {
		t.Fatalf("a token prefix was counted as a second literal: %v", err)
	}
	if err := validateTranslation("Defaults with --driver=none.", "默认使用 --driver=none"); err != nil {
		t.Fatalf("sentence punctuation became part of an assignment: %v", err)
	}
	if err := validateTranslation("Restrict search to file contents or file path: {file|path}", "将搜索范围限制为文件内容或文件路径：{file|path}"); err != nil {
		t.Fatalf("translated prose was counted as an enum occurrence: %v", err)
	}
	if err := validateTranslation("CNI plug-in to use. Valid options: auto, bridge, calico, cilium, flannel, kindnet, or path to a CNI manifest (default: auto)", "使用的 CNI 插件。可选值：auto、bridge、calico、cilium、flannel、kindnet，或 CNI 清单文件路径（默认：auto）"); err != nil {
		t.Fatalf("a prose alternative was treated as an enum value: %v", err)
	}
	for _, pair := range [][2]string{
		{"Use --force", "使用 --force-all"},
		{"Send SIGKILL", "发送 SIGKILLL"},
		{"Coloring: auto, always, never", "着色模式：自动、总是、从不"},
		{"Read /dev/stdin", "读取 / dev/stdin"},
		{"Certificate file; default server2.pem", "证书文件；默认服务器2.pem"},
		{"Driver: [fish, cmd, powershell]", "驱动：[鱼, cmd, powershell]"},
		{"Valid options: auto, bridge, calico, cilium", "可选值：auto、桥接、calico、cilium"},
		{"Plugin policy: 'AllowAll', 'DenyAll' or 'Allowlist'", "插件策略：'AllowAll'、'DenyAll' 或 'AllowList'"},
		{"[auto no yes only]", "[auto no 只有]"},
		{"Use 3DES_CBC for keys", "密钥使用 3DES_ CBC"},
		{"Enable (tls1>=1) mode", "启用 (tls1 1) 模式"},
		{"Available values: [KubeProxyConfiguration KubeletConfiguration]", "可选值：[KubeProxy Configation KubeletConfiguration]"},
		{"Stop the %{product} server", "停止产品服务器"},
		{"Template: {{.Release.Name}}", "模板：Release Name"},
		{"Defaults to false.", "默认为虚假"},
		{"Use systemd as cgroup manager", "使用容器运行时作为 cgroup 管理器"},
		{"Restrict search to file contents or file path: {file|path}", "将搜索范围限制为文件内容或文件路径：{file|路径}"},
		{"Delay (ns|us|ms|s|m|h)", "延迟 (ns us ms s m h)"},
		{"Choose {true|false}", "选择 {true 或 false}"},
	} {
		if err := validateTranslation(pair[0], pair[1]); err == nil {
			t.Fatalf("altered technical literal was accepted: %q -> %q", pair[0], pair[1])
		}
	}
	_, err := readTranslations(strings.NewReader("english\tzh-CN\tcontext\nUse --force\t使用\ttool\nSend SIGKILL\t发送\ttool\n"))
	if err == nil || !strings.Contains(err.Error(), "--force") || !strings.Contains(err.Error(), "SIGKILL") {
		t.Fatalf("validation did not report all invalid entries: %v", err)
	}
	if err := validateTranslation("Read /dev/stdin; valid options: bridge, calico", "读取输入，选择网络"); err == nil || !strings.Contains(err.Error(), "/dev/stdin") || !strings.Contains(err.Error(), "calico") {
		t.Fatalf("validation did not report all invalid literals: %v", err)
	}
}
