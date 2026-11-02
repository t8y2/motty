package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"motty/internal/ai/llm"
	"os"
	"runtime"
	"strings"
	"testing"
)

var testPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func imageConversation() llm.Request {
	return llm.Request{Model: "default", Messages: []llm.Message{
		{Role: llm.RoleUser, Text: "look", Images: []llm.Image{{MediaType: llm.ImagePNG, Data: testPNG}}},
		{Role: llm.RoleAssistant, Text: "ok"},
		{Role: llm.RoleUser, Text: "again", Images: []llm.Image{{MediaType: llm.ImagePNG, Data: testPNG}}},
	}}
}

func TestCodexAndOpenCodeReadPicturesFromPrivateFiles(t *testing.T) {
	for kind, flag := range map[string]string{"codex-cli": "--image=", "opencode-cli": "--file="} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			cmd, err := buildCommand(kind, command{program: "agent"}, imageConversation(), dir, "http://127.0.0.1:1/mcp", "token", nil)
			if err != nil {
				t.Fatal(err)
			}
			var files []string
			for _, arg := range cmd.args {
				if path, ok := strings.CutPrefix(arg, flag); ok {
					files = append(files, path)
				}
			}
			if len(files) != 2 {
				t.Fatalf("picture flags = %v in %v", files, cmd.args)
			}
			for _, path := range files {
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, testPNG) {
					t.Fatalf("%s: %v", path, err)
				}
				// Unix permission bits do not map to Windows ACLs, where a
				// fresh file always reads as 0666.
				if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
					t.Fatalf("%s is %v", path, info.Mode())
				}
			}
			if kind == "codex-cli" && cmd.args[len(cmd.args)-1] != "-" {
				t.Fatalf("the prompt placeholder must stay last: %v", cmd.args)
			}
		})
	}
}

func TestNoPicturesAddNoFlags(t *testing.T) {
	for _, kind := range []string{"claude-code-cli", "codex-cli", "opencode-cli"} {
		cmd, err := buildCommand(kind, command{program: "agent"}, llm.Request{Model: "default"}, t.TempDir(), "http://127.0.0.1:1/mcp", "token", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, arg := range cmd.args {
			if strings.HasPrefix(arg, "--image") || strings.HasPrefix(arg, "--file") || arg == "--input-format" {
				t.Fatalf("%s: unexpected %s", kind, arg)
			}
		}
	}
}

func TestClaudeGetsPicturesAndPromptInOneStreamJSONMessage(t *testing.T) {
	input, err := claudeImageInput("PROMPT", imageConversation().Messages[0].Images)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(input, "\n") || strings.Count(input, "\n") != 1 {
		t.Fatalf("not exactly one JSON line: %q", input)
	}
	var message struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type   string `json:"type"`
				Text   string `json:"text"`
				Source struct {
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
				} `json:"source"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(input), &message); err != nil {
		t.Fatal(err)
	}
	blocks := message.Message.Content
	if message.Type != "user" || message.Message.Role != "user" || len(blocks) != 2 ||
		blocks[0].Type != "image" || blocks[0].Source.MediaType != llm.ImagePNG ||
		blocks[0].Source.Data != base64.StdEncoding.EncodeToString(testPNG) ||
		blocks[1].Type != "text" || blocks[1].Text != "PROMPT" {
		t.Fatalf("message = %+v", message)
	}
}

func TestPicturesMustBeWhatTheyClaim(t *testing.T) {
	bad := []llm.Image{{MediaType: llm.ImageJPEG, Data: testPNG}}
	if _, err := claudeImageInput("x", bad); err == nil {
		t.Fatal("claude accepted a mislabeled picture")
	}
	if _, err := writeImages(t.TempDir(), bad); err == nil {
		t.Fatal("a mislabeled picture was written")
	}
}
