package chrome

import "SnakeMedia/internal/browser"

// Short names for the shared Chrome helpers. The protocol code here, including
// recaptcha.go (which is not edited), calls them by these names.
var (
	cdpCall        = browser.Call
	evaluate       = browser.Evaluate
	localJSON      = browser.LocalJSON
	localWebSocket = browser.LocalWebSocket
	readBounded    = browser.ReadBounded
)
