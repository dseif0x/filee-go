// Package bundle makes the files of Filee.app and of its system integration:
// Info.plist, the launch agent, the Services menu status, the .icns icon and
// the universal (two-architecture) executable. Pure Go, so the app is
// packaged on any OS (scripts/build-macos-app.sh runs it on Linux).
package bundle

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// BundleID is the app's bundle identifier (the original's, so permissions and
// services keep their entries).
const BundleID = "com.filee.app"

const (
	// ServiceMenuItem is the Services menu title in Info.plist; ServicesMenu.strings translates it.
	ServiceMenuItem = "Convert with Filee"
	// ServiceMessage is the selector macOS sends, without its argument labels (NSMessage).
	ServiceMessage = "convertFiles"
	// ServicePort is NSPortName.
	ServicePort = "Filee"
)

// Value is a property-list value: string, bool, int, []Value or Dict.
type Value any

// Dict is a property-list dictionary with its keys in order.
type Dict []Entry

// Entry is one key of a Dict.
type Entry struct {
	Key   string
	Value Value
}

// Plist writes a property list as XML.
func Plist(root Value) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n")
	write(&b, root, 0)
	b.WriteString("</plist>\n")
	return b.String()
}

func write(b *strings.Builder, v Value, depth int) {
	ind := strings.Repeat("\t", depth)
	switch x := v.(type) {
	case string:
		fmt.Fprintf(b, "%s<string>%s</string>\n", ind, html.EscapeString(x))
	case bool:
		if x {
			fmt.Fprintf(b, "%s<true/>\n", ind)
		} else {
			fmt.Fprintf(b, "%s<false/>\n", ind)
		}
	case int:
		fmt.Fprintf(b, "%s<integer>%d</integer>\n", ind, x)
	case []Value:
		fmt.Fprintf(b, "%s<array>\n", ind)
		for _, e := range x {
			write(b, e, depth+1)
		}
		fmt.Fprintf(b, "%s</array>\n", ind)
	case []string:
		vals := make([]Value, len(x))
		for i, s := range x {
			vals[i] = s
		}
		write(b, vals, depth)
	case Dict:
		if len(x) == 0 {
			fmt.Fprintf(b, "%s<dict/>\n", ind)
			return
		}
		fmt.Fprintf(b, "%s<dict>\n", ind)
		for _, e := range x {
			fmt.Fprintf(b, "%s\t<key>%s</key>\n", ind, html.EscapeString(e.Key))
			write(b, e.Value, depth+1)
		}
		fmt.Fprintf(b, "%s</dict>\n", ind)
	default:
		panic(fmt.Sprintf("bundle: unsupported property-list value %T", v))
	}
}

// LaunchAgent is ~/Library/LaunchAgents/com.filee.app.plist: starts Filee in
// the background at sign-in (macOS 12 and older, or a copy outside a bundle,
// where SMAppService isn't available).
func LaunchAgent(executable string) string {
	return Plist(Dict{
		{"Label", BundleID},
		{"AssociatedBundleIdentifiers", []string{BundleID}},
		{"ProgramArguments", []string{executable, "app", "--background"}},
		{"LimitLoadToSessionType", "Aqua"},
		{"RunAtLoad", true},
		{"KeepAlive", false},
	})
}

// ServiceStatusKey is the service's entry in the pbs NSServicesStatus
// preferences ("bundle id - menu title - message"), quoted for defaults.
func ServiceStatusKey() string {
	return fmt.Sprintf("\"%s - %s - %s\"", BundleID, ServiceMenuItem, ServiceMessage)
}

// ServiceStatus is the service switched on or off in the right-click menu
// and the Services menu, as System Settings writes it.
func ServiceStatus(enabled bool) string {
	on := 0
	if enabled {
		on = 1
	}
	return fmt.Sprintf(`{ "enabled_context_menu" = %d; "enabled_services_menu" = %d; "presentation_modes" = { "ContextMenu" = %d; "ServicesMenu" = %d; }; }`, on, on, on, on)
}

// ServiceTitles are the Services menu title in each localization (the .lproj folders).
var ServiceTitles = map[string]string{
	"en": "Convert with Filee", "de": "Mit Filee konvertieren", "ko": "Filee로 변환",
	"pt-BR": "Converter com o Filee", "pt-PT": "Converter com o Filee",
	"zh-Hans": "用 Filee 转换", "zh-Hant": "用 Filee 轉換",
}

// ServicesStrings is a ServicesMenu.strings file (UTF-8, which macOS reads).
func ServicesStrings(title string) string {
	return "/* Finder's right-click menu: the title of Filee's service (NSServices in Info.plist). */\n" +
		fmt.Sprintf("%q = %q;\n", ServiceMenuItem, title)
}

// Localizations are the .lproj folders, sorted, English first.
func Localizations() []string {
	var out []string
	for k := range ServiceTitles {
		if k != "en" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return append([]string{"en"}, out...)
}

// InfoPlist is Contents/Info.plist of Filee.app. The app is an agent
// (LSUIElement): no Dock icon or menu bar until the settings ask for the
// Dock icon (it then switches its activation policy itself).
func InfoPlist(version, executable string) string {
	short := version
	if i := strings.IndexAny(short, "-+"); i > 0 {
		short = short[:i]
	}
	langs := make([]Value, 0)
	for _, l := range Localizations() {
		langs = append(langs, l)
	}
	return Plist(Dict{
		{"CFBundleDevelopmentRegion", "en"},
		{"CFBundleLocalizations", langs},
		{"CFBundleIdentifier", BundleID},
		{"CFBundleName", "Filee"},
		{"CFBundleDisplayName", "Filee"},
		{"CFBundleExecutable", executable},
		{"CFBundlePackageType", "APPL"},
		{"CFBundleShortVersionString", short},
		{"CFBundleVersion", short},
		{"CFBundleIconFile", "filee.icns"},
		{"LSMinimumSystemVersion", "13.0"},
		{"LSUIElement", true},
		{"LSApplicationCategoryType", "public.app-category.utilities"},
		{"NSHighResolutionCapable", true},
		{"NSSupportsAutomaticGraphicsSwitching", true},
		// "Convert with Filee" in Finder's right-click menu, answered by the running Filee.
		{"NSServices", []Value{Dict{
			{"NSMenuItem", Dict{{"default", ServiceMenuItem}}},
			{"NSMessage", ServiceMessage},
			{"NSPortName", ServicePort},
			{"NSRequiredContext", Dict{}},
			{"NSSendFileTypes", []string{"public.item"}},
		}}},
		{"NSAppleEventsUsageDescription", "Filee reads selected Finder files when you use a conversion shortcut."},
		{"NSInputMonitoringUsageDescription", "Filee detects your conversion shortcut and drag gesture to show the conversion menu."},
		{"NSUserNotificationAlertStyle", "banner"},
	})
}

// Entitlements are for codesign (hardened runtime): Apple events to ask Finder for its selection.
func Entitlements() string {
	return Plist(Dict{{"com.apple.security.automation.apple-events", true}})
}
