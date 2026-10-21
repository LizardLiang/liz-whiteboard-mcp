# Headless Chromium sidecar for get_board_image / get_canvas_board_image.
#
# The MCP server drives it over the DevTools protocol (LIZ_RENDER_CDP_URL) and
# the browser loads the app's /render/... pages. The base image listens on 9222
# for DevTools; the compose file exposes that port to the compose network only.
#
# fonts-noto-cjk: boards hold Chinese text, and the app's font stack names
# "Noto Sans CJK TC". Without it every CJK glyph renders as an empty box.
FROM chromedp/headless-shell:156.0.8078.36

RUN apt-get update \
    && apt-get install -y --no-install-recommends fonts-noto-cjk \
    && rm -rf /var/lib/apt/lists/*

EXPOSE 9222
