package desktop

// Forwardable lists the agent-desktop commands that make sense to send
// into the shadow session. Anything else is still permitted on main
// but rejected for pip before it reaches IPC.
var Forwardable = map[string]bool{
	"snapshot": true, "find": true, "get": true, "is": true,
	"click": true, "double-click": true, "triple-click": true, "right-click": true,
	"type": true, "set-value": true, "clear": true, "focus": true,
	"select": true, "toggle": true, "check": true, "uncheck": true,
	"expand": true, "collapse": true, "scroll": true, "scroll-to": true,
	"press": true, "key-down": true, "key-up": true,
	"hover": true, "drag": true,
	"mouse-move": true, "mouse-click": true, "mouse-down": true,
	"mouse-up": true, "mouse-wheel": true,
	"screenshot": true, "launch": true, "close-app": true,
	"list-windows": true, "list-displays": true, "list-apps": true,
	"focus-window": true, "resize-window": true, "move-window": true,
	"minimize": true, "maximize": true, "restore": true,
	"list-surfaces": true, "open-system-surface": true,
	"clipboard-get": true, "clipboard-set": true, "clipboard-clear": true,
	"wait": true, "batch": true,
}
