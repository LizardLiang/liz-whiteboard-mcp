package tools

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The app repo is a sibling checkout. The test reads its DEFAULT_ELEMENT_STYLE
// so the Go mirror cannot drift silently; it skips when the sibling is absent.
const appSceneFile = "../../../liz-whiteboard/src/lib/canvas-engine/scene.ts"

func TestCanvasStyleDefaults_MatchTheApp(t *testing.T) {
	src, err := os.ReadFile(appSceneFile)
	if err != nil {
		t.Skipf("app checkout not found at %s", appSceneFile)
	}
	text := string(src)
	start := strings.Index(text, "export const DEFAULT_ELEMENT_STYLE")
	require.GreaterOrEqual(t, start, 0, "DEFAULT_ELEMENT_STYLE not found")
	body := text[start:]
	end := strings.Index(body, "\n}\n")
	require.GreaterOrEqual(t, end, 0)
	body = body[:end]

	// Two-space-indented `key: value,` lines; comment lines never match.
	entry := regexp.MustCompile(`(?m)^  (\w+): (.+),$`)
	app := map[string]any{}
	for _, m := range entry.FindAllStringSubmatch(body, -1) {
		app[m[1]] = parseTSLiteral(t, m[1], m[2])
	}
	require.NotEmpty(t, app)
	assert.Equal(t, app, canvasStyleDefaults)
}

// parseTSLiteral decodes the literal forms DEFAULT_ELEMENT_STYLE uses.
func parseTSLiteral(t *testing.T, key, lit string) any {
	t.Helper()
	switch {
	case lit == "null":
		return nil
	case lit == "true":
		return true
	case lit == "false":
		return false
	case lit == "[]":
		return []any{}
	case strings.HasPrefix(lit, "'") && strings.HasSuffix(lit, "'"):
		return lit[1 : len(lit)-1]
	}
	n, err := strconv.ParseFloat(lit, 64)
	require.NoError(t, err, "unsupported literal for %s: %s", key, lit)
	var viaJSON any
	require.NoError(t, json.Unmarshal([]byte(lit), &viaJSON))
	return n
}
