//go:build darwin || windows || linux

package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"motty/internal/buildinfo"
	"motty/internal/crashlog"
	"motty/internal/datadir"
	"motty/internal/i18n"
)

// logSession is the open log and crash report of this run. It is nil until the
// process has become the one that owns the data directory.
var logSession *crashlog.Session

// logDirectory is where this run keeps its log and crash reports.
func logDirectory() string {
	dir := datadir.Current().Dir
	if dir == "" {
		var err error
		if dir, err = defaultDataDir(); err != nil {
			return ""
		}
	}
	return crashlog.Dir(dir)
}

// startLogging keeps what the program logs, and what the Go runtime prints when
// it dies, in the data directory. A Windows program has no standard error and a
// crashed one leaves no system report, so without these files a user can only
// say that it closed. A failure to open them is logged and costs nothing else.
func startLogging() {
	dir := logDirectory()
	if dir == "" {
		return
	}
	session, err := crashlog.Start(dir)
	if err != nil {
		log.Printf("motty: no log files: %v", err)
		return
	}
	logSession = session
	log.SetOutput(io.MultiWriter(os.Stderr, recentLog, session))
	app.SetFaultHandler(func(fault app.Fault) {
		log.Printf("motty: caught a panic in %s: %s\n%s", fault.Where, fault.Message, fault.Stack)
	})
	// A native fault (a graphics driver, a fast-fail) ends the process before
	// the Go runtime can write anything. Windows can keep a dump of it.
	switch dumps, err := app.EnableCrashDumps(`Motty\CrashDumps`); {
	case err == nil:
		log.Printf("motty: native crash dumps go to %s", dumps)
	case !errors.Is(err, app.ErrUnsupported):
		log.Printf("motty: no native crash dumps: %v", err)
	}
	if session.PreviousCrash != "" {
		log.Printf("motty: the previous run ended abnormally; report kept as %s", session.PreviousCrash)
	}
}

// previousCrash is the crash report the previous run left, if any.
func previousCrash() string {
	if logSession == nil {
		return ""
	}
	return logSession.PreviousCrash
}

// stopLogging ends the run cleanly, so the next one does not take it for a crash.
func stopLogging() { logSession.Close() }

// fatal reports why the program cannot go on, in the log and in a message box,
// and ends the process. A graphical program that only writes to standard error
// has no way to say it: its window flickers and is gone.
func fatal(err error) {
	log.Print(err)
	if os.Getenv("VIVA_HEADLESS") == "" {
		title, body := fatalText(launchLanguage(), err, logDirectory())
		app.ShowFatalError(title, body)
	}
	stopLogging()
	os.Exit(1)
}

func fatalText(language i18n.Language, err error, logDir string) (title, body string) {
	if language == i18n.Chinese {
		title = buildinfo.ProductName + " 无法启动"
		body = fmt.Sprintf("%v", err)
		if logDir != "" {
			body += "\n\n日志保存在:\n" + filepath.ToSlash(logDir) + "\n反馈问题时请附上其中的 motty.log。"
		}
		return title, body
	}
	if language == i18n.TraditionalChinese {
		title = buildinfo.ProductName + " 無法啟動"
		body = fmt.Sprintf("%v", err)
		if logDir != "" {
			body += "\n\n記錄儲存在：\n" + filepath.ToSlash(logDir) + "\n回報問題時請附上其中的 motty.log。"
		}
		return title, body
	}
	title = buildinfo.ProductName + " could not start"
	body = fmt.Sprintf("%v", err)
	if logDir != "" {
		body += "\n\nThe log is kept in:\n" + filepath.ToSlash(logDir) + "\nPlease attach motty.log to a bug report."
	}
	return title, body
}

// crashNoticeFor gives the crash report of the previous run to the window that
// tells the user about it: the first one of this run.
func crashNoticeFor(place placement) string {
	if !place.rememberFrame {
		return ""
	}
	return previousCrash()
}
