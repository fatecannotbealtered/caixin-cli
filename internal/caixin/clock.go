package caixin

import "time"

// timeNow is the clock every calendar-relative decision in this package reads:
// whether a surface is serving stale content, and whether a dated URL points at
// a past year. Production leaves it as time.Now.
//
// It exists because those decisions are recorded into the fixture goldens under
// testdata/. A cassette replays the same bytes forever, but "is this article
// older than 30 days" answered against the wall clock does not stay the same —
// a golden recorded with stale_content:false flips to true once the article
// ages past the window, and the suite starts failing on the calendar instead of
// on a code change. Pinning the clock during replay keeps the goldens a
// statement about the parser rather than about today's date.
var timeNow = time.Now

// SetNow pins the clock read by timeNow and returns a function that restores
// the previous one. Only fixture replay uses it; this package is under
// internal/, so exporting it cannot widen the module's public surface.
//
// It writes a package-level variable, so callers must not run replay tests in
// parallel with anything that depends on the clock.
func SetNow(fn func() time.Time) (restore func()) {
	previous := timeNow
	timeNow = fn
	return func() { timeNow = previous }
}
