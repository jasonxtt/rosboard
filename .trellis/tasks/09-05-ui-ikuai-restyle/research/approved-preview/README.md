# Frozen compact study approved in the conversation

These four files are byte-for-byte copies of the compact study created on 2026-09-05 and approved after the user requested narrower navigation. Source: the ignored local `.codex/tmp/ui-style-study-20260905/` directory. This is not the root `preview-ikuai/` artifact used by another agent.

The study uses `style.css?v=compact-nav-2`. Open `index.html` through a static server, select the dual-column layout and inspect overview, terminals, routing wizard, target library, access and settings. Single-column and theme controls in the top study toolbar are review aids, not production feature requirements.

From the repository root, on a host that owns the requested LAN address and has port 8791 free:

```sh
python3 -m http.server 8791 --bind 10.0.0.86 --directory .trellis/tasks/09-05-ui-ikuai-restyle/research/approved-preview
```

Then open http://10.0.0.86:8791/. On another host use its own bind address, or use `127.0.0.1` for local-only viewing. Do not start a second server if the port is occupied. At the time of planning, the existing local service serves the identical original copy; this checkpoint does not alter that service. No launchd configuration or logs are included.

Data is synthetic and interactions are illustrative/in-memory. Some buttons display a toast, some pages are not drawn, and forms omit real advanced fields. This is a visual reference, not a functional replacement, runnable app acceptance, or authorization to remove controls. Do not send its example forms to an API.

## Subsequent overview amendment

The user later selected the branch implementation’s four header cards and quick-link module, with the study’s device-information panel below the latter. See [the overview amendment](../overview-reference-amendment.md). This frozen preview (and the original 8791 service) still shows the original study; it has not been edited to show that combination. All other sections, particularly routing-policy selectors and layout, remain authoritative.

## Frozen file hashes (SHA-256)

| File | SHA-256 |
|---|---|
| `index.html` | `d93066c5de76da2e29e8c92acbb42d411071ea301f1b5543403d6b143c107684` |
| `style.css` | `abecaf234fd18ae2c472bb6217f5f9ea636e4c3c9ea43c28a088188132bd7467` |
| `app.js` | `1a0ce4f29b495d37ff21e15abc88fa93dfca0b8c059bbda6485fde6b6d82ffa4` |
| `mark.svg` | `e96d020a96d06176be97e90d8145f78e4caa1c101597d70b0e7acd53ffac473d` |
