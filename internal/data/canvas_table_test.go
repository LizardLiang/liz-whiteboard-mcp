package data

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCanvasTable(t *testing.T) {
	table, err := ParseCanvasTable(JSONText(`{"kind":"table","table":{"rows":[{"id":"r","height":40}],` +
		`"cols":[{"id":"c","width":120}],"cells":{"r:c":{"text":"hi","fill":"#fff","textRuns":[{"end":2}]}},"headerRow":true}}`))
	require.NoError(t, err)
	assert.Equal(t, "hi", table.CellText("r", "c"))
	assert.Equal(t, "#fff", table.Cells["r:c"].Fill)
	assert.Equal(t, "", table.CellText("r", "missing"))
	assert.True(t, table.HeaderRow)
}

func TestParseCanvasTable_Refusals(t *testing.T) {
	for _, raw := range []string{`not json`, `{"kind":"rectangle"}`, `{"kind":"table"}`, ``} {
		_, err := ParseCanvasTable(JSONText(raw))
		assert.ErrorIs(t, err, ErrNotATable, raw)
	}
	table, err := ParseCanvasTable(JSONText(`{"kind":"table","table":{"rows":[],"cols":[],"headerRow":false}}`))
	require.NoError(t, err)
	assert.NotNil(t, table.Cells, "Cells is never nil")
}
