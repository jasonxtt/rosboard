# Memory input default unit, 2026-10-15

Resource inputs now show 内存 high(MB) and 内存 max(MB). Users enter numeric
MB values; the shared editor sends explicit RouterOS M suffixes (1024² bytes).
Existing quantities display in MB, while untouched API values stay unchanged.
Blank values inherit defaults; turning resources off preserves hidden inputs.

Verified: full Go tests, 101 frontend tests, final targeted container tests,
TypeScript/Vite build, lint (only existing FastTrack key warnings), dual UI
bootstrap/CSS isolation. Test service and production run dev-memory-mb-1015;
health and all 79 embedded assets match. Production replacement followed a
complete verified NAS backup in 20261015T083043Z-container-feedback. No container
operation or existing job modification was performed. User visual acceptance
remains pending; task stays in_progress and PR Draft.
