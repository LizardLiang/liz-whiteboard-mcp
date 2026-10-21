package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/appapi"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/render"
)

const imgBoardID = "11111111-2222-4333-8444-555555555555"
const imgFocusID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

type fakeRenderer struct {
	called int
	req    render.Request
	res    *render.Result
	err    error
}

func (f *fakeRenderer) Render(_ context.Context, mint render.Minter, req render.Request) (*render.Result, error) {
	f.called++
	f.req = req
	if _, err := mint(context.Background(), req.Kind, req.BoardID); err != nil {
		return nil, err
	}
	return f.res, f.err
}

type imgHarness struct {
	deps     boardImageDeps
	fake     *fakeRenderer
	mints    int
	authErr  error
	mintErr  error
	notFound string
}

func newImgHarness() *imgHarness {
	h := &imgHarness{fake: &fakeRenderer{res: &render.Result{Images: []render.Image{{
		PNG: []byte("png"), Label: "Whole board.", WorldRect: render.Rect{X: 1, Y: 2, Width: 3, Height: 4},
	}}}}}
	h.deps = boardImageDeps{
		authorize: func(context.Context, string, string) error { return h.authErr },
		mint: func(_ context.Context, _, _, _, nf string) (appapi.RenderToken, error) {
			h.mints++
			h.notFound = nf
			return appapi.RenderToken{Token: "t", ExpiresAt: time.Now().Add(time.Minute)}, h.mintErr
		},
		renderer: func() boardImageRenderer { return h.fake },
	}
	return h
}

func (h *imgHarness) run(args boardImageArgs, id string) *mcp.CallToolResult {
	res, _, _ := runBoardImage(context.Background(), h.deps, appapi.RenderKindWhiteboard,
		"whiteboardId", id, "Whiteboard "+id+" not found.", args)
	return res
}

func errCode(t *testing.T, res *mcp.CallToolResult) (string, string) {
	t.Helper()
	require.True(t, res.IsError)
	var p struct{ Code, Message, Field string }
	require.NoError(t, json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &p))
	return p.Code, p.Field
}

func sp(s string) *string { return &s }

func TestBoardImage_ArgValidation(t *testing.T) {
	cases := []struct {
		name  string
		args  boardImageArgs
		id    string
		field string
	}{
		{"bad board id", boardImageArgs{}, "nope", "whiteboardId"},
		{"bad mode", boardImageArgs{Mode: sp("zoom")}, imgBoardID, "mode"},
		{"bad theme", boardImageArgs{Theme: sp("sepia")}, imgBoardID, "theme"},
		{"bad focus id", boardImageArgs{FocusIDs: []string{"x"}}, imgBoardID, "focusIds"},
		{"focus with tiles", boardImageArgs{Mode: sp("tiles"), FocusIDs: []string{imgFocusID}}, imgBoardID, "focusIds"},
		{"too many focus ids", boardImageArgs{FocusIDs: make([]string, 51)}, imgBoardID, "focusIds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newImgHarness()
			code, field := errCode(t, h.run(tc.args, tc.id))
			assert.Equal(t, "VALIDATION_ERROR", code)
			assert.Equal(t, tc.field, field)
			assert.Zero(t, h.mints)
			assert.Zero(t, h.fake.called)
		})
	}
}

func TestBoardImage_AccessDeniedBeforeMint(t *testing.T) {
	h := newImgHarness()
	h.authErr = mcperr.New(mcperr.NotFound, "Whiteboard x not found.")
	code, _ := errCode(t, h.run(boardImageArgs{}, imgBoardID))
	assert.Equal(t, "NOT_FOUND", code)
	assert.Zero(t, h.mints, "no token may be minted for a board the caller cannot read")
	assert.Zero(t, h.fake.called)
}

func TestBoardImage_MintNotFoundIsPassedThrough(t *testing.T) {
	h := newImgHarness()
	h.mintErr = mcperr.New(mcperr.NotFound, "Whiteboard gone not found.")
	code, _ := errCode(t, h.run(boardImageArgs{}, imgBoardID))
	assert.Equal(t, "NOT_FOUND", code)
	assert.Equal(t, "Whiteboard "+imgBoardID+" not found.", h.notFound)
}

func TestBoardImage_ReturnsImageAndWorldRect(t *testing.T) {
	h := newImgHarness()
	res := h.run(boardImageArgs{Theme: sp("dark"), FocusIDs: []string{imgFocusID}}, imgBoardID)
	require.False(t, res.IsError)
	require.Len(t, res.Content, 2)
	img, ok := res.Content[0].(*mcp.ImageContent)
	require.True(t, ok)
	assert.Equal(t, "image/png", img.MIMEType)
	assert.Equal(t, []byte("png"), img.Data)
	assert.Contains(t, res.Content[1].(*mcp.TextContent).Text, "x=1 y=2 width=3 height=4")
	assert.Equal(t, "dark", h.fake.req.Theme)
	assert.Equal(t, []string{imgFocusID}, h.fake.req.FocusIDs)
	assert.Equal(t, render.ModeFit, h.fake.req.Mode)
}

func TestBoardImage_EmptyBoardIsTextOnly(t *testing.T) {
	h := newImgHarness()
	h.fake.res = &render.Result{Empty: true}
	res := h.run(boardImageArgs{}, imgBoardID)
	require.False(t, res.IsError)
	require.Len(t, res.Content, 1)
	assert.Equal(t, "board is empty", res.Content[0].(*mcp.TextContent).Text)
}

func TestBoardImage_RendererErrorIsReported(t *testing.T) {
	h := newImgHarness()
	h.fake.err = errors.New("boom")
	res := h.run(boardImageArgs{}, imgBoardID)
	code, _ := errCode(t, res)
	assert.Equal(t, "INTERNAL_ERROR", code)
}
