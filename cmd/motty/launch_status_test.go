//go:build darwin || windows || linux

package main

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	"motty/internal/buildinfo"
)

func TestSecondaryLaunchOnlyAnnouncesAnUnstartedDevelopmentBuild(t *testing.T) {
	tests := []struct {
		language  string
		unstarted string
	}{
		{language: "en", unstarted: "this development build did not start"},
		{language: "zh-CN", unstarted: "本次开发构建未启动"},
		{language: "zh-TW", unstarted: "本次開發建置未啟動"},
	}
	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			t.Setenv(languageVariable, test.language)
			var output bytes.Buffer
			explainSecondaryLaunch(&output)
			text := output.String()
			if !buildinfo.Development {
				if text != "" {
					t.Fatalf("release build announced a handoff: %q", text)
				}
				return
			}
			if !strings.Contains(text, buildinfo.ProductName) || !strings.Contains(text, test.unstarted) ||
				!strings.Contains(text, "motty: secondary launch handed off") {
				t.Fatalf("%s handoff = %q", test.language, text)
			}
		})
	}
}

func TestStartedIsReportedOnlyAfterAWindowWasCreated(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	reportStarted(errors.New("window creation failed"))
	if strings.Contains(output.String(), "started") {
		t.Fatalf("a failed window was reported as started: %q", output.String())
	}
	reportStarted(nil)
	if !strings.Contains(output.String(), "motty: started") {
		t.Fatalf("a created window was not reported: %q", output.String())
	}
}
