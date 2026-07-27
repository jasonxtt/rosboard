# Implementation Plan

1. Backend terminal scope
   - Add scoped discovery filtering in `internal/service/monitor.go`.
   - Add unit coverage for ARP/DHCP discovery outside terminal CIDRs.

2. Frontend layout/copy
   - Move overview range pills into the topbar control cluster.
   - Remove the standalone range row and update metric card CSS.
   - Change terminal table action copy to `编辑`.

3. Verify and deploy
   - Run Go tests and frontend build.
   - Rebuild the embedded Go binary.
   - Deploy to `10.0.0.6` and verify dashboard data plus served frontend assets.
