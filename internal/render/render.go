// Package render turns a board into PNG screenshots by driving a headless
// Chromium sidecar over the Chrome DevTools Protocol.
//
// The sidecar opens the app's own /render/<kind>/<token> page, so what the
// caller sees is what a person sees: the same components, fonts, and theme.
// This package never builds a URL from caller input. The host comes from
// LIZ_RENDER_APP_URL, the path from a fixed kind, and the query from validated
// enums and ids, so a caller cannot steer the browser to another site.
//
// Env vars (read at call time):
//
//	LIZ_RENDER_CDP_URL  DevTools endpoint of the sidecar (default http://localhost:9222)
//	LIZ_RENDER_APP_URL  Origin the SIDECAR uses to reach the app (default http://localhost:3000)
package render

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/appapi"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const (
	// ViewportSize is the square viewport in CSS pixels. 1568 is the long-edge
	// size Claude accepts without downscaling.
	ViewportSize = 1568
	// MaxTiles caps how many images one call returns.
	MaxTiles = 12
	// ReadyTimeout bounds one page load plus render.
	ReadyTimeout = 15 * time.Second
	// minTokenLife is the least remaining token life worth starting a page
	// load with. A load may take ReadyTimeout, but the token is only checked
	// when the page boots, so a small margin covers the navigation itself.
	minTokenLife = 8 * time.Second
)

// Modes a caller can request.
const (
	ModeFit   = "fit"
	ModeTiles = "tiles"
)

// Sentinel failures from a Capturer.
var (
	// ErrUnreachable means the DevTools endpoint could not be dialled.
	ErrUnreachable = errors.New("renderer unreachable")
	// ErrTimeout means the page never set a ready or error marker in time.
	ErrTimeout = errors.New("renderer timed out waiting for the page")
)

// Rect is a rectangle in board (world) coordinates.
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// TileGrid describes how the board divides into tiles at zoom 1.0.
type TileGrid struct {
	Cols     int      `json:"cols"`
	Rows     int      `json:"rows"`
	TileW    float64  `json:"tileWidth"`
	TileH    float64  `json:"tileHeight"`
	OriginX  float64  `json:"originX"`
	OriginY  float64  `json:"originY"`
	Occupied [][2]int `json:"occupied"`
}

// Page is what one page load reports.
type Page struct {
	// PNG is the screenshot of the render root; nil when none was requested.
	PNG []byte
	// Error is the page's data-render-error value, or "".
	Error string
	// Empty is true when the board has nothing to draw.
	Empty     bool
	WorldRect *Rect
	Grid      *TileGrid
}

// Capturer loads one page and reports what it shows.
type Capturer interface {
	Capture(ctx context.Context, pageURL string, screenshot bool) (*Page, error)
}

// Minter issues a render token for one board on behalf of the caller.
type Minter func(ctx context.Context, kind, boardID string) (appapi.RenderToken, error)

// Request is one validated image request.
type Request struct {
	Kind     string // appapi.RenderKindWhiteboard or RenderKindCanvas
	BoardID  string
	Mode     string // ModeFit or ModeTiles
	FocusIDs []string
	Theme    string // "light" or "dark"
}

// Image is one returned screenshot.
type Image struct {
	PNG       []byte
	Label     string
	WorldRect Rect
}

// Result holds the images for one request. Empty is true for a board with
// nothing to draw, in which case Images is empty.
type Result struct {
	Images []Image
	Empty  bool
}

// Renderer drives a Capturer against the app's render pages.
type Renderer struct {
	Capturer Capturer
	// AppURL is the origin the sidecar uses to reach the app.
	AppURL string
	// Now is overridable for tests.
	Now func() time.Time
}

func (r *Renderer) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// tokenSource hands out render tokens and re-mints before one runs short.
type tokenSource struct {
	r     *Renderer
	mint  Minter
	req   Request
	token appapi.RenderToken
	have  bool
}

func (s *tokenSource) get(ctx context.Context, forceFresh bool) (string, error) {
	if forceFresh || !s.have || s.token.ExpiresAt.Sub(s.r.now()) < minTokenLife {
		tok, err := s.mint(ctx, s.req.Kind, s.req.BoardID)
		if err != nil {
			return "", err
		}
		s.token, s.have = tok, true
	}
	return s.token.Token, nil
}

// pageURL builds the render page URL for one framing.
func (r *Renderer) pageURL(token string, req Request, query url.Values) string {
	query.Set("theme", req.Theme)
	return strings.TrimRight(r.AppURL, "/") + "/render/" + req.Kind + "/" + url.PathEscape(token) + "?" + query.Encode()
}

// load captures one page, retrying once with a fresh token when the page
// reports the token as bad or expired.
func (r *Renderer) load(ctx context.Context, src *tokenSource, req Request, query url.Values, screenshot bool) (*Page, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := src.get(ctx, attempt > 0)
		if err != nil {
			return nil, err
		}
		page, err := r.Capturer.Capture(ctx, r.pageURL(token, req, query), screenshot)
		if err != nil {
			return nil, mapCaptureError(err)
		}
		if page.Error == "invalid-token" || page.Error == "token-expired" {
			continue
		}
		return page, nil
	}
	return nil, mcperr.New(mcperr.InternalError,
		"The renderer rejected a fresh render token twice. Check that the app and the renderer share the same signing configuration.")
}

func mapCaptureError(err error) error {
	switch {
	case errors.Is(err, ErrTimeout):
		return mcperr.New(mcperr.ConnectionError,
			fmt.Sprintf("The board renderer did not finish drawing within %s.", ReadyTimeout))
	case errors.Is(err, ErrUnreachable):
		return mcperr.New(mcperr.ConnectionError,
			"Cannot reach the board renderer (headless Chromium). Start the renderer service, or set LIZ_RENDER_CDP_URL to its DevTools address.")
	default:
		return mcperr.New(mcperr.ConnectionError, "The board renderer failed: "+err.Error())
	}
}

// pageError maps a page error marker to an MCP error. ok is false when the
// page carries no error.
func pageError(marker string) (error, bool) {
	switch {
	case marker == "":
		return nil, false
	case strings.HasPrefix(marker, "unknown-focus:"):
		id := strings.TrimPrefix(marker, "unknown-focus:")
		return mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("focusIds contains %s, which is not on this board.", id), "focusIds"), true
	case marker == "focus-empty":
		return mcperr.NewField(mcperr.ValidationError,
			"focusIds matched nothing that can be drawn.", "focusIds"), true
	default:
		return mcperr.New(mcperr.InternalError, "The board renderer reported: "+marker), true
	}
}

// Render produces the images for one request.
func (r *Renderer) Render(ctx context.Context, mint Minter, req Request) (*Result, error) {
	src := &tokenSource{r: r, mint: mint, req: req}
	if req.Mode == ModeTiles {
		return r.renderTiles(ctx, src, req)
	}

	query := url.Values{"mode": {"fit"}}
	if len(req.FocusIDs) > 0 {
		query = url.Values{"mode": {"focus"}, "focus": {strings.Join(req.FocusIDs, ",")}}
	}
	page, err := r.load(ctx, src, req, query, true)
	if err != nil {
		return nil, err
	}
	if page.Error == "empty-board" || page.Empty {
		return &Result{Empty: true}, nil
	}
	if e, bad := pageError(page.Error); bad {
		return nil, e
	}
	if page.WorldRect == nil || len(page.PNG) == 0 {
		return nil, mcperr.New(mcperr.InternalError, "The board renderer returned no image.")
	}
	label := "Whole board."
	if len(req.FocusIDs) > 0 {
		label = fmt.Sprintf("Focused on %d item(s).", len(req.FocusIDs))
	}
	return &Result{Images: []Image{{PNG: page.PNG, WorldRect: *page.WorldRect, Label: label}}}, nil
}

func (r *Renderer) renderTiles(ctx context.Context, src *tokenSource, req Request) (*Result, error) {
	// One fit pass without a screenshot reports the grid for the whole board.
	probe, err := r.load(ctx, src, req, url.Values{"mode": {"fit"}}, false)
	if err != nil {
		return nil, err
	}
	if probe.Error == "empty-board" || probe.Empty {
		return &Result{Empty: true}, nil
	}
	if e, bad := pageError(probe.Error); bad {
		return nil, e
	}
	grid := probe.Grid
	if grid == nil || len(grid.Occupied) == 0 {
		return nil, mcperr.New(mcperr.InternalError, "The board renderer returned no tile grid.")
	}
	if n := len(grid.Occupied); n > MaxTiles {
		return nil, mcperr.NewField(mcperr.ValidationError,
			fmt.Sprintf("This board needs %d tiles, more than the limit of %d. "+
				"Pass focusIds to look at part of the board, or use mode \"fit\".", n, MaxTiles), "mode")
	}

	images := make([]Image, 0, len(grid.Occupied))
	for _, rc := range grid.Occupied {
		query := url.Values{"mode": {"tile"}, "tile": {fmt.Sprintf("%d,%d", rc[0], rc[1])}}
		page, err := r.load(ctx, src, req, query, true)
		if err != nil {
			return nil, err
		}
		if e, bad := pageError(page.Error); bad {
			return nil, e
		}
		if len(page.PNG) == 0 {
			return nil, mcperr.New(mcperr.InternalError, "The board renderer returned no image for a tile.")
		}
		world := Rect{
			X:      grid.OriginX + float64(rc[1])*grid.TileW,
			Y:      grid.OriginY + float64(rc[0])*grid.TileH,
			Width:  grid.TileW,
			Height: grid.TileH,
		}
		if page.WorldRect != nil {
			world = *page.WorldRect
		}
		images = append(images, Image{
			PNG:       page.PNG,
			WorldRect: world,
			Label:     fmt.Sprintf("Tile row %d, column %d of a %d x %d grid.", rc[0], rc[1], grid.Rows, grid.Cols),
		})
	}
	return &Result{Images: images}, nil
}
