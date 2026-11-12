package tools

import "io"

// ToolsetNames lists every tagged tool name, sorted. Tests compare it with the
// registered tools.
func ToolsetNames() []string { return sortedKeys(toolToolset) }

// GuideToolNames lists the tools that have a guide entry, sorted.
func GuideToolNames() []string { return sortedKeys(toolGuides) }

// SetPanicLog points the panic log at w and returns a func that restores it.
func SetPanicLog(w io.Writer) func() {
	prev := panicLog
	panicLog = w
	return func() { panicLog = prev }
}
