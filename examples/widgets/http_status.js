// An example script widget: whether a web endpoint answers, and how
// fast. Tap checks again. See docs/script-widgets.md.
//
//   widget:
//     type: http_status
//     url: https://example.com/health
//     title: API          # default: the host
//     poll_ms: 30000
//
//   widgets:
//     http_status:
//       timeout: 5000     # ms, shared by every http_status key

const LIVE = "#2ecc40";
const DOWN = "#e74c3c";
const UNKNOWN = "#777777";

widget({
  pollMS: 30000,
  minPollMS: 5000,

  validate(params) {
    if (!/^https?:\/\//.test(String(params.url ?? ""))) {
      throw new Error("url must start with http:// or https://");
    }
  },

  create(w) {
    const url = String(w.params.url);
    const title = String(w.params.title ?? "").trim() || url.replace(/^https?:\/\//, "").split(/[/:?#]/)[0];
    const timeout = w.settings.timeout ?? 5000;
    let state = null; // {up, ms, status} after the first check

    async function check() {
      const started = Date.now();
      try {
        const r = await udeck.http.request({ url, timeout });
        state = { up: r.status < 400, ms: Date.now() - started, status: r.status };
      } catch (err) {
        state = { up: false, ms: null, status: null };
        throw err; // udeck logs it
      }
    }

    return {
      poll: check,
      gesture: check,

      draw(c) {
        const color = state === null ? UNKNOWN : state.up ? LIVE : DOWN;
        c.icon(state?.up === false ? "lucide/cloud-off" : "lucide/activity", 50, 20, 96, 96, color);
        c.text(title.toUpperCase(), 8, 126, c.width - 16, 30, { size: 26, color: "#f0f0f0" });
        let detail = "...";
        if (state !== null) detail = state.up ? `${state.ms} MS` : state.status ? `HTTP ${state.status}` : "DOWN";
        c.text(detail, 8, 156, c.width - 16, 26, { size: 22, color });
      },
    };
  },
});
