package tools

// ToolsetNames lists every tagged tool name, sorted. Tests compare it with the
// registered tools.
func ToolsetNames() []string { return sortedKeys(toolToolset) }

// GuideToolNames lists the tools that have a guide entry, sorted.
func GuideToolNames() []string { return sortedKeys(toolGuides) }
