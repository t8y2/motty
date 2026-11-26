package configformat

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDetectKind(t *testing.T) {
	cases := []struct {
		lang string
		want Kind
	}{
		{"json", KindJSON},
		{"JSON", KindJSON},
		{"yaml", KindYAML},
		{"yml", KindYAML},
		{"toml", KindTOML},
		{"go", KindNone},
		{"", KindNone},
	}
	for _, tc := range cases {
		if got := DetectKind(tc.lang); got != tc.want {
			t.Fatalf("DetectKind(%q) = %q, want %q", tc.lang, got, tc.want)
		}
	}
}

func TestFormatJSONPreservesKeyOrderAndReportsSyntaxError(t *testing.T) {
	input := `{"z":1,"a":{"d":true,"b":[1,2]}}`
	got, err := Format(KindJSON, input, 2)
	if err != nil {
		t.Fatalf("Format(JSON) error = %v", err)
	}
	want := "{\n  \"z\": 1,\n  \"a\": {\n    \"d\": true,\n    \"b\": [\n      1,\n      2\n    ]\n  }\n}\n"
	if got != want {
		t.Fatalf("Format(JSON) =\n%s\nwant\n%s", got, want)
	}
	// 幂等性
	again, err := Format(KindJSON, got, 2)
	if err != nil || again != got {
		t.Fatalf("Format(JSON) not idempotent: %q, err=%v", again, err)
	}

	_, err = Format(KindJSON, "{\n  \"ok\": 1,\n  \"bad\": \n}", 2)
	var ferr *FormatError
	if !errors.As(err, &ferr) || ferr.Line != 4 {
		t.Fatalf("Format(invalid JSON) = %#v, want FormatError at line 4", err)
	}
}

func TestFormatYAMLPreservesBlankLinesCommentsOrderAndReportsError(t *testing.T) {
	input := strings.Join([]string{
		"# top comment",
		"services:",
		"    web:",
		"        image: nginx",
		"",
		"        # ports section",
		"        ports:",
		"            - \"80:80\"",
		"",
		"    db:",
		"        image: postgres",
		"        script: |",
		"            echo hello",
		"              echo indented",
		"",
	}, "\n")
	got, err := Format(KindYAML, input, 2)
	if err != nil {
		t.Fatalf("Format(YAML) error = %v", err)
	}
	want := strings.Join([]string{
		"# top comment",
		"services:",
		"  web:",
		"    image: nginx",
		"",
		"    # ports section",
		"    ports:",
		"      - \"80:80\"",
		"",
		"  db:",
		"    image: postgres",
		"    script: |",
		"      echo hello",
		"        echo indented",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("Format(YAML) =\n%q\nwant\n%q", got, want)
	}

	// 序列项内联映射（- path / methods:）必须与 path 对齐，不能误加一个 indentWidth 导致语法损坏
	seqInput := strings.Join([]string{
		"routes:",
		"    - path: /upload",
		"      methods:",
		"          - POST",
		"          - PUT",
		"    - path: /download",
		"      methods:",
		"          - GET",
		"",
	}, "\n")
	got4, err := Format(KindYAML, seqInput, 4)
	if err != nil {
		t.Fatalf("Format(YAML seq 4) error = %v", err)
	}
	if got4 != seqInput {
		t.Fatalf("Format(YAML seq 4) =\n%q\nwant\n%q", got4, seqInput)
	}
	got2, err := Format(KindYAML, seqInput, 2)
	if err != nil {
		t.Fatalf("Format(YAML seq 2) error = %v", err)
	}
	want2 := strings.Join([]string{
		"routes:",
		"  - path: /upload",
		"    methods:",
		"      - POST",
		"      - PUT",
		"  - path: /download",
		"    methods:",
		"      - GET",
		"",
	}, "\n")
	if got2 != want2 {
		t.Fatalf("Format(YAML seq 2) =\n%q\nwant\n%q", got2, want2)
	}

	// 块标量带显式缩进时重排会改变值，必须原样保留；引号内的 ": |" 不能误判为块标量
	blockEdge := strings.Join([]string{
		"msg: \"a: | b\"",
		"next: 1",
		"doc: |2",
		"      indented first",
		"    less indented second",
		"",
	}, "\n")
	gotEdge, err := Format(KindYAML, blockEdge, 2)
	if err != nil {
		t.Fatalf("Format(YAML blockEdge) error = %v", err)
	}
	wantEdge := blockEdge // re-indenting would change the scalar, so it stays as written
	if gotEdge != wantEdge {
		t.Fatalf("Format(YAML blockEdge) =\n%q\nwant\n%q", gotEdge, wantEdge)
	}

	_, err = Format(KindYAML, "a: 1\nb:\n  - x\n bad: [", 2)
	var ferr *FormatError
	if !errors.As(err, &ferr) || ferr.Line < 1 {
		t.Fatalf("Format(invalid YAML) = %#v, want FormatError", err)
	}
}

func TestFormatTOMLPreservesCommentsOrderAndReportsSemanticErrors(t *testing.T) {
	input := "# global\ntitle = \"motty\"\n[server]\nhost=\"127.0.0.1\"\nports = [\n8080,\n8443,\n]\n# tls comment\n[server.tls]\nenabled=true # keep\n"
	got, err := Format(KindTOML, input, 2)
	if err != nil {
		t.Fatalf("Format(TOML) error = %v", err)
	}
	want := "# global\ntitle = \"motty\"\n\n[server]\nhost = \"127.0.0.1\"\nports = [\n  8080,\n  8443,\n]\n# tls comment\n[server.tls]\nenabled = true # keep\n"
	if got != want {
		t.Fatalf("Format(TOML) =\n%q\nwant\n%q", got, want)
	}
	again, err := Format(KindTOML, got, 2)
	if err != nil || again != got {
		t.Fatalf("Format(TOML) not idempotent: %q, err=%v", again, err)
	}

	// 非法裸值 a = nope 与重复键 a = 1 / a = 2 必须报错
	for _, invalid := range []string{
		"a = nope\n",
		"a = 1\na = 2\n",
		"[server]\nhost = \"unterminated\n",
	} {
		if _, err := Format(KindTOML, invalid, 2); err == nil {
			t.Fatalf("Format(invalid TOML %q) unexpectedly succeeded", invalid)
		}
	}

	// 多行字符串与 ']' 在同一行闭合时（含 TOML 合法的 4 引号闭合 """"），后续表头不得被误缩进
	sameLineClose := "items = [\n'''\nhello\n''']\nquoted = [\n\"\"\"\nworld\"\"\"\"]\n[t]\na = 1\n"
	gotSameLine, err := Format(KindTOML, sameLineClose, 2)
	if err != nil {
		t.Fatalf("Format(sameLineClose) error = %v", err)
	}
	if !strings.Contains(gotSameLine, "\n[t]\n") {
		t.Fatalf("subsequent table header was wrongly indented:\n%q", gotSameLine)
	}
}

func TestFoldRangesJSON(t *testing.T) {
	src := "{\n  \"a\": {\n    \"x\": 1\n  },\n  \"b\": [\n    1,\n    2\n  ]\n}\n"
	got := FoldRanges(KindJSON, src)
	want := []FoldRange{
		{Header: 0, LastLine: 7},
		{Header: 1, LastLine: 2},
		{Header: 4, LastLine: 6},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FoldRanges(JSON) = %+v, want %+v", got, want)
	}
}

func TestFoldRangesYAML(t *testing.T) {
	src := "server:\n  host: localhost\n  tls:\n    cert: |\n      line1\n      line2\nlogging:\n  level: info\n"
	got := FoldRanges(KindYAML, src)
	want := []FoldRange{
		{Header: 0, LastLine: 5},
		{Header: 2, LastLine: 5},
		{Header: 3, LastLine: 5},
		{Header: 6, LastLine: 7},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FoldRanges(YAML) = %+v, want %+v", got, want)
	}
}

func TestFoldRangesTOMLNestedDottedHeadersAndMultiline(t *testing.T) {
	src := strings.Join([]string{
		"[server]",         // 0
		"host = '0.0.0.0'", // 1
		"ports = [",        // 2
		"  80,",            // 3
		"  443",            // 4
		"]",                // 5
		"[server.tls]",     // 6
		"enabled = true",   // 7
		"[logging]",        // 8
		"banner = '''",     // 9
		"hello",            // 10
		"'''",              // 11
	}, "\n")
	got := FoldRanges(KindTOML, src)
	want := []FoldRange{
		{Header: 0, LastLine: 7},
		{Header: 2, LastLine: 4},
		{Header: 6, LastLine: 7},
		{Header: 8, LastLine: 11},
		{Header: 9, LastLine: 11},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FoldRanges(TOML) = %+v, want %+v", got, want)
	}
}

// A block scalar with an explicit indentation indicator reads its body relative
// to that indicator, so re-indenting it would change the value: the source
// must come back untouched.
func TestFormatYAMLNeverChangesTheDecodedValue(t *testing.T) {
	src := "a: |2\n      one\n    two\nb:\n    c: 1\n"
	got, err := Format(KindYAML, src, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !sameYAMLValues(src, got) {
		t.Fatalf("formatting changed the decoded value:\n%s", got)
	}
}
