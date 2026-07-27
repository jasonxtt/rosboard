# Terminal CIDR and overview layout fixes

## Goal

Make terminal CIDR scope behave as an operator-visible terminal discovery boundary, and tighten the overview header/card layout so the deployed panel matches the requested screenshot adjustments.

## Requirements

- Terminal monitor must not create non-router terminal rows from DHCP/ARP/IPv6-neighbor discovery when the address is outside the selected device's terminal CIDRs.
- RouterOS self remains a single self row and may still show exact router-assigned addresses from other interfaces.
- Rename the terminal table action from `编辑终端` to `编辑`; keep the dialog title unchanged.
- Move the overview range selector into the topbar control cluster immediately before `系统正常`; remove the standalone `时间范围` row.
- Keep the range selector visually compact and aligned with the existing topbar status controls.
- Make the four overview metric card sparklines wider and slightly higher, while preserving value/detail readability.
- Align the online-terminal and active-connection footers with the CPU/memory metric card footers.
- Build, verify, and deploy the finished panel to `10.0.0.6:8080`.

## Acceptance Criteria

- [ ] With default terminal CIDRs excluding `10.0.2.0/24` and `10.0.3.0/24`, `/api/dashboard?device=default` no longer contains non-router terminals `10.0.2.1` or `10.0.3.2`.
- [ ] RouterOS self still appears once when router-owned addresses are present.
- [ ] Terminal table row action reads `编辑`.
- [ ] Overview range selector is in the topbar and `时间范围` is absent.
- [ ] Metric card sparklines use more horizontal space and card footers are aligned across the four cards.
- [ ] Go tests and frontend production build pass.
- [ ] The updated binary is deployed and serving on `http://10.0.0.6:8080/`.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
