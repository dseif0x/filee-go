//go:build darwin

package gui

import (
	"os/exec"
	"strings"

	"github.com/ebitengine/purego/objc"

	"github.com/dseif0x/filee-go/internal/gui/radial"
	"github.com/dseif0x/filee-go/internal/jobs"
	"github.com/dseif0x/filee-go/internal/macos/macplatform"
	"github.com/dseif0x/filee-go/internal/macos/ns"
)

// The original's toast: a job the ring handed off (still running after a
// few seconds, failed, or replaced by a new gesture) says how it ended. Here
// that is a notification (UNUserNotificationCenter, which only works for an
// app bundle; elsewhere osascript's "display notification"). Clicking it
// shows the result in Finder.

type notifier struct {
	m      *macApp
	center ns.ID
}

var (
	notifyDelegateClass objc.Class
	authBlock           objc.Block
)

func newNotifier(m *macApp) *notifier {
	n := &notifier{m: m}
	// UNUserNotificationCenter raises an exception in a process without a bundle identifier.
	if !macplatform.InBundle() || ns.Class("UNUserNotificationCenter") == 0 {
		return n
	}
	n.center = ns.Send(ns.Class("UNUserNotificationCenter"), "currentNotificationCenter")
	if notifyDelegateClass == 0 {
		var err error
		notifyDelegateClass, err = objc.RegisterClass("FileeNotificationDelegate", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: ns.Sel("userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:"),
				Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, response objc.ID, done objc.Block) {
					info := ns.Send(ns.Send(ns.Send(ns.Send(response, "notification"), "request"), "content"), "userInfo")
					if path := ns.GoString(ns.Send(info, "objectForKey:", ns.String("path"))); path != "" && current != nil {
						current.mac.RevealInFileManager(path)
					}
					done.Invoke()
				}},
			{Cmd: ns.Sel("userNotificationCenter:willPresentNotification:withCompletionHandler:"),
				Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, _ objc.ID, done objc.Block) {
					// Shown while Filee is in front too: banner, list and (macOS 11 and older) alert.
					done.Invoke(uint(1<<4 | 1<<3 | 1<<2))
				}},
		})
		if err != nil {
			m.log.Warn("notification delegate", "error", err)
			return n
		}
	}
	ns.Send(n.center, "setDelegate:", ns.Send(ns.ID(notifyDelegateClass), "new"))
	if authBlock == 0 {
		authBlock = objc.NewBlock(func(_ objc.Block, granted bool, err objc.ID) {
			if !granted {
				m.log.Info("notifications not allowed", "error", ns.ErrorText(err))
			}
		})
	}
	// Alert and sound.
	ns.Send(n.center, "requestAuthorizationWithOptions:completionHandler:", uint(1<<2|1<<1), authBlock)
	return n
}

// Notify shows how a handed-off job ended (radial.Host).
func (m *macApp) Notify(s jobs.Snapshot) {
	title, body := radial.NotificationText(m.app, s)
	path := ""
	for _, f := range s.Files {
		if len(f.Outputs) > 0 {
			path = f.Outputs[0]
			break
		}
	}
	m.notifier.show(s.ID, title, body, path)
}

func (n *notifier) show(id, title, body, path string) {
	if n.center == 0 {
		esc := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) }
		script := `display notification "` + esc(body) + `" with title "` + esc(title) + `"`
		go func() { _ = exec.Command("/usr/bin/osascript", "-e", script).Run() }()
		return
	}
	content := ns.Send(ns.New("UNMutableNotificationContent"), "autorelease")
	ns.Send(content, "setTitle:", ns.String(title))
	ns.Send(content, "setBody:", ns.String(body))
	if path != "" {
		ns.Send(content, "setUserInfo:", ns.Send(ns.Class("NSDictionary"), "dictionaryWithObject:forKey:", ns.String(path), ns.String("path")))
	}
	req := ns.Send(ns.Class("UNNotificationRequest"), "requestWithIdentifier:content:trigger:", ns.String("job-"+id), content, objc.ID(0))
	ns.Send(n.center, "addNotificationRequest:withCompletionHandler:", req, objc.ID(0))
}
