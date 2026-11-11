//go:build darwin || windows || linux

package main

import (
	"reflect"
	"testing"
)

type openFileRegistrar struct {
	handler func(string)
}

func (registrar *openFileRegistrar) OnOpenFile(handler func(string)) {
	registrar.handler = handler
}

func TestRegisterOpenFileUsesANewerFramework(t *testing.T) {
	registrar := &openFileRegistrar{}
	var got []string
	registerOpenFile(registrar, func(path string) { got = append(got, path) })
	if registrar.handler == nil {
		t.Fatal("the opened-file handler was not registered")
	}
	registrar.handler("/tmp/notes.md")
	if want := []string{"/tmp/notes.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("opened files = %q, want %q", got, want)
	}
}

func TestRegisterOpenFileAcceptsAnOlderFramework(t *testing.T) {
	registerOpenFile(struct{}{}, func(string) {})
}
