//go:build darwin || windows || linux

package main

// registerOpenFile uses the document callback when the linked Viva version
// provides it. Motty's CI pins Viva separately from release builds, so this
// capability check lets the application compile before the Viva change lands
// while enabling it automatically with a newer framework.
func registerOpenFile(application any, handler func(string)) {
	if registrar, ok := application.(interface{ OnOpenFile(func(string)) }); ok {
		registrar.OnOpenFile(handler)
	}
}
