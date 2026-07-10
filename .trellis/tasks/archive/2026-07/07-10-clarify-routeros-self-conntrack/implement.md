# Implementation plan

1. Add typed `seen-reply`/`assured` fields through RouterOS → service → API → frontend.
2. Add RouterOS-specific conntrack naming and summary counts without changing LAN terminal semantics.
3. Add Winbox-style flags and explicit state text to connection details.
4. Add regression tests for raw flag projection and RouterOS label/state behavior.
5. Run full checks, rebuild embedded assets and binary, then restore `0.0.0.0:8080` if runtime credentials can be recovered safely.
