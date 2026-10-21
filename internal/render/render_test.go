package render

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/appapi"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

type fakeCapturer struct {
	urls  []string
	shots []bool
	reply func(u *url.URL, n int) (*Page, error)
}

func (f *fakeCapturer) Capture(_ context.Context, pageURL string, screenshot bool) (*Page, error) {
	f.urls = append(f.urls, pageURL)
	f.shots = append(f.shots, screenshot)
	u, _ := url.Parse(pageURL)
	return f.reply(u, len(f.urls))
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newRenderer(f *fakeCapturer, c *clock) (*Renderer, *int, Minter) {
	mints := 0
	r := &Renderer{Capturer: f, AppURL: "http://app:3000/", Now: c.Now}
	mint := func(context.Context, string, string) (appapi.RenderToken, error) {
		mints++
		return appapi.RenderToken{Token: "tok", ExpiresAt: c.now.Add(60 * time.Second)}, nil
	}
	return r, &mints, mint
}

func okPage() *Page {
	return &Page{PNG: []byte("png"), WorldRect: &Rect{Width: 10, Height: 10}}
}

func mcpErr(t *testing.T, err error) *mcperr.McpError {
	t.Helper()
	var me *mcperr.McpError
	require.ErrorAs(t, err, &me)
	return me
}

var req = Request{Kind: "whiteboard", BoardID: "b", Mode: ModeFit, Theme: "light"}

func TestRender_FitURL(t *testing.T) {
	f := &fakeCapturer{reply: func(*url.URL, int) (*Page, error) { return okPage(), nil }}
	r, _, mint := newRenderer(f, &clock{time.Now()})
	res, err := r.Render(context.Background(), mint, req)
	require.NoError(t, err)
	require.Len(t, res.Images, 1)
	u, _ := url.Parse(f.urls[0])
	assert.Equal(t, "app:3000", u.Host)
	assert.Equal(t, "/render/whiteboard/tok", u.Path)
	assert.Equal(t, "fit", u.Query().Get("mode"))
	assert.Equal(t, "light", u.Query().Get("theme"))
}

func TestRender_FocusMapsToModeFocus(t *testing.T) {
	f := &fakeCapturer{reply: func(*url.URL, int) (*Page, error) { return okPage(), nil }}
	r, _, mint := newRenderer(f, &clock{time.Now()})
	rq := req
	rq.FocusIDs = []string{"a", "b"}
	_, err := r.Render(context.Background(), mint, rq)
	require.NoError(t, err)
	u, _ := url.Parse(f.urls[0])
	assert.Equal(t, "focus", u.Query().Get("mode"))
	assert.Equal(t, "a,b", u.Query().Get("focus"))
}

func TestRender_UnknownFocusIsValidationNamingIDs(t *testing.T) {
	f := &fakeCapturer{reply: func(*url.URL, int) (*Page, error) { return &Page{Error: "unknown-focus:zzz"}, nil }}
	r, _, mint := newRenderer(f, &clock{time.Now()})
	_, err := r.Render(context.Background(), mint, req)
	me := mcpErr(t, err)
	assert.Equal(t, mcperr.ValidationError, me.Code)
	assert.Equal(t, "focusIds", me.Field)
	assert.Contains(t, me.Message, "zzz")
}

func TestRender_EmptyBoard(t *testing.T) {
	for _, p := range []*Page{{Empty: true}, {Error: "empty-board"}} {
		f := &fakeCapturer{reply: func(*url.URL, int) (*Page, error) { return p, nil }}
		r, _, mint := newRenderer(f, &clock{time.Now()})
		res, err := r.Render(context.Background(), mint, req)
		require.NoError(t, err)
		assert.True(t, res.Empty)
		assert.Empty(t, res.Images)
	}
}

func TestRender_BadTokenRetriesOnceThenInternalError(t *testing.T) {
	f := &fakeCapturer{reply: func(*url.URL, int) (*Page, error) { return &Page{Error: "token-expired"}, nil }}
	r, mints, mint := newRenderer(f, &clock{time.Now()})
	_, err := r.Render(context.Background(), mint, req)
	assert.Equal(t, mcperr.InternalError, mcpErr(t, err).Code)
	assert.Equal(t, 2, *mints)
	assert.Len(t, f.urls, 2)
}

func TestRender_BadTokenThenGoodRecovers(t *testing.T) {
	f := &fakeCapturer{reply: func(_ *url.URL, n int) (*Page, error) {
		if n == 1 {
			return &Page{Error: "invalid-token"}, nil
		}
		return okPage(), nil
	}}
	r, mints, mint := newRenderer(f, &clock{time.Now()})
	res, err := r.Render(context.Background(), mint, req)
	require.NoError(t, err)
	assert.Len(t, res.Images, 1)
	assert.Equal(t, 2, *mints)
}

func TestRender_CapturerFailuresNameTheRenderer(t *testing.T) {
	for _, sentinel := range []error{ErrUnreachable, ErrTimeout} {
		f := &fakeCapturer{reply: func(*url.URL, int) (*Page, error) { return nil, sentinel }}
		r, _, mint := newRenderer(f, &clock{time.Now()})
		_, err := r.Render(context.Background(), mint, req)
		me := mcpErr(t, err)
		assert.Equal(t, mcperr.ConnectionError, me.Code)
		assert.Contains(t, strings.ToLower(me.Message), "renderer")
	}
	f := &fakeCapturer{reply: func(*url.URL, int) (*Page, error) { return nil, errors.New("x") }}
	r, _, mint := newRenderer(f, &clock{time.Now()})
	_, err := r.Render(context.Background(), mint, req)
	assert.Contains(t, mcpErr(t, err).Message, "renderer")
}

func gridPage(occupied [][2]int) *Page {
	return &Page{WorldRect: &Rect{Width: 1568, Height: 1568}, Grid: &TileGrid{
		Cols: 4, Rows: 4, TileW: 1568, TileH: 1568, OriginX: 100, OriginY: 200, Occupied: occupied,
	}}
}

func TestRender_TilesOverCapIsValidationWithCount(t *testing.T) {
	occ := make([][2]int, 13)
	f := &fakeCapturer{reply: func(*url.URL, int) (*Page, error) { return gridPage(occ), nil }}
	r, _, mint := newRenderer(f, &clock{time.Now()})
	rq := req
	rq.Mode = ModeTiles
	_, err := r.Render(context.Background(), mint, rq)
	me := mcpErr(t, err)
	assert.Equal(t, mcperr.ValidationError, me.Code)
	assert.Contains(t, me.Message, "13")
	assert.Contains(t, me.Message, "focusIds")
	assert.Len(t, f.urls, 1, "no tile may be loaded once the cap is exceeded")
}

func TestRender_TilesProbeThenOneLoadPerOccupiedTile(t *testing.T) {
	occ := [][2]int{{0, 0}, {1, 2}}
	f := &fakeCapturer{reply: func(u *url.URL, _ int) (*Page, error) {
		if u.Query().Get("mode") == "fit" {
			return gridPage(occ), nil
		}
		return okPage(), nil
	}}
	r, mints, mint := newRenderer(f, &clock{time.Now()})
	rq := req
	rq.Mode = ModeTiles
	res, err := r.Render(context.Background(), mint, rq)
	require.NoError(t, err)
	require.Len(t, res.Images, 2)
	assert.False(t, f.shots[0], "the grid probe takes no screenshot")
	u, _ := url.Parse(f.urls[2])
	assert.Equal(t, "tile", u.Query().Get("mode"))
	assert.Equal(t, "1,2", u.Query().Get("tile"))
	assert.Contains(t, res.Images[1].Label, "row 1, column 2")
	assert.Equal(t, 1, *mints, "one token covers loads inside its life")
}

func TestRender_TilesRemintWhenTokenRunsShort(t *testing.T) {
	c := &clock{time.Now()}
	f := &fakeCapturer{}
	f.reply = func(u *url.URL, _ int) (*Page, error) {
		c.now = c.now.Add(30 * time.Second) // each load eats half a token life
		if u.Query().Get("mode") == "fit" {
			return gridPage([][2]int{{0, 0}, {0, 1}, {0, 2}}), nil
		}
		return okPage(), nil
	}
	r, mints, mint := newRenderer(f, c)
	rq := req
	rq.Mode = ModeTiles
	_, err := r.Render(context.Background(), mint, rq)
	require.NoError(t, err)
	assert.Greater(t, *mints, 1)
}

func TestRender_TilesEmptyBoard(t *testing.T) {
	f := &fakeCapturer{reply: func(*url.URL, int) (*Page, error) { return &Page{Empty: true}, nil }}
	r, _, mint := newRenderer(f, &clock{time.Now()})
	rq := req
	rq.Mode = ModeTiles
	res, err := r.Render(context.Background(), mint, rq)
	require.NoError(t, err)
	assert.True(t, res.Empty)
}
