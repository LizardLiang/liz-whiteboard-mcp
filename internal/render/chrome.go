package render

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

const (
	defaultCDPURL = "http://localhost:9222"
	defaultAppURL = "http://localhost:3000"
)

// CDPURL resolves the sidecar's DevTools address at call time.
func CDPURL() string {
	if v := os.Getenv("LIZ_RENDER_CDP_URL"); v != "" {
		return v
	}
	return defaultCDPURL
}

// AppURL resolves the origin the sidecar uses to reach the app.
func AppURL() string {
	if v := os.Getenv("LIZ_RENDER_APP_URL"); v != "" {
		return v
	}
	return defaultAppURL
}

// New returns a Renderer wired to the env-configured sidecar.
func New() *Renderer {
	return &Renderer{Capturer: &ChromeCapturer{}, AppURL: AppURL()}
}

// readStateJS reports the render root's markers as one JSON string.
const readStateJS = `(() => {
  const el = document.querySelector('[data-render-root]');
  if (!el) return JSON.stringify({});
  const parse = (name) => { const v = el.getAttribute(name); return v ? JSON.parse(v) : null; };
  return JSON.stringify({
    error: el.getAttribute('data-render-error') || '',
    empty: el.getAttribute('data-board-empty') === 'true',
    worldRect: parse('data-world-rect'),
    grid: parse('data-tile-grid'),
  });
})()`

const readySelector = `[data-render-root][data-render-ready="true"], [data-render-root][data-render-error]`

// ChromeCapturer is the chromedp-backed Capturer. The zero value reads
// LIZ_RENDER_CDP_URL on each capture.
type ChromeCapturer struct {
	// Endpoint overrides LIZ_RENDER_CDP_URL when set.
	Endpoint string
}

// Capture loads pageURL in a fresh tab of the sidecar and reads the page state.
func (c *ChromeCapturer) Capture(ctx context.Context, pageURL string, screenshot bool) (*Page, error) {
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = CDPURL()
	}
	wsURL, err := debuggerWebSocketURL(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}

	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, wsURL, chromedp.NoModifyURL)
	defer cancelAlloc()
	tabCtx, cancelTab := chromedp.NewContext(allocCtx)
	defer cancelTab()

	// Open the tab first so a dead endpoint reads as unreachable, not as a timeout.
	if err := chromedp.Run(tabCtx); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}

	runCtx, cancelRun := context.WithTimeout(tabCtx, ReadyTimeout)
	defer cancelRun()

	var stateJSON string
	var png []byte
	actions := chromedp.Tasks{
		emulation.SetDeviceMetricsOverride(ViewportSize, ViewportSize, 1, false),
		chromedp.Navigate(pageURL),
		chromedp.WaitVisible(readySelector, chromedp.ByQuery),
		chromedp.Evaluate(readStateJS, &stateJSON),
	}
	if err := chromedp.Run(runCtx, actions); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		return nil, err
	}

	var state struct {
		Error     string    `json:"error"`
		Empty     bool      `json:"empty"`
		WorldRect *Rect     `json:"worldRect"`
		Grid      *TileGrid `json:"grid"`
	}
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil, fmt.Errorf("unreadable page state: %w", err)
	}
	page := &Page{Error: state.Error, Empty: state.Empty, WorldRect: state.WorldRect, Grid: state.Grid}

	if screenshot && page.Error == "" && !page.Empty {
		if err := chromedp.Run(runCtx, chromedp.Screenshot(`[data-render-root]`, &png, chromedp.ByQuery)); err != nil {
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				return nil, ErrTimeout
			}
			return nil, err
		}
		page.PNG = png
	}
	return page, nil
}

// debuggerWebSocketURL turns the DevTools endpoint into a browser WebSocket URL.
//
// chromedp/headless-shell answers a DevTools request only when its Host header
// is an IP address or "localhost". A compose service name ("renderer:9222")
// fails that check, on /json/version and on the WebSocket upgrade alike. So the
// host is resolved to an IP here and every request, including the upgrade the
// allocator makes, uses the IP. The returned URL is then dialled as-is
// (chromedp.NoModifyURL) rather than re-resolved.
func debuggerWebSocketURL(ctx context.Context, endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid LIZ_RENDER_CDP_URL %q", endpoint)
	}
	host, port := u.Hostname(), u.Port()
	if port == "" {
		port = "9222"
	}
	if net.ParseIP(host) == nil {
		resolveCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		addrs, err := net.DefaultResolver.LookupIPAddr(resolveCtx, host)
		if err != nil || len(addrs) == 0 {
			return "", fmt.Errorf("cannot resolve %s", host)
		}
		host = addrs[0].IP.String()
	}
	hostPort := net.JoinHostPort(host, port)

	if u.Scheme == "ws" || u.Scheme == "wss" {
		u.Host = hostPort
		return u.String(), nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+hostPort+"/json/version", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("DevTools endpoint answered HTTP %d", resp.StatusCode)
	}
	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&version); err != nil || version.WebSocketDebuggerURL == "" {
		return "", errors.New("DevTools endpoint returned no webSocketDebuggerUrl")
	}
	ws, err := url.Parse(version.WebSocketDebuggerURL)
	if err != nil {
		return "", err
	}
	// Keep the browser path; always dial the IP:port we resolved.
	return "ws://" + hostPort + strings.TrimSuffix(ws.Path, "/"), nil
}
