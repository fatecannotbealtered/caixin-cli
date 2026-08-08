package caixin

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// FindBrowser locates an installed Chrome or Edge.
//
// chromedp's default allocator only searches PATH plus a few names, and on
// Windows neither browser is normally on PATH -- so a machine with Chrome
// installed still looked browserless. The standard install locations are
// checked explicitly, and CAIXIN_BROWSER overrides everything for anyone whose
// layout is unusual.
func FindBrowser() string {
	if explicit := os.Getenv("CAIXIN_BROWSER"); explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit
		}
	}
	for _, name := range []string{"chrome", "google-chrome", "chromium", "msedge", "microsoft-edge"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	for _, candidate := range browserCandidates() {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func browserCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		var roots []string
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			if value := os.Getenv(env); value != "" {
				roots = append(roots, value)
			}
		}
		var out []string
		for _, root := range roots {
			out = append(out,
				filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
			)
		}
		return out
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		return []string{
			"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser",
			"/usr/bin/microsoft-edge", "/snap/bin/chromium",
		}
	}
}

// FindBrowserForTest exposes discovery so a test can confirm it really found
// nothing before asserting the no-browser path.
func FindBrowserForTest() string { return FindBrowser() }
