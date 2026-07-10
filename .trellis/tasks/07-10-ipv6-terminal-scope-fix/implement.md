# IPv6 terminal scope fix implementation plan

1. Add IPv6 address REST types/client and local-network derivation.
2. Add family-specific terminal-detail projections and normalization tests.
3. Make list address and detail scope follow All/IPv4/IPv6 entry state.
4. Validate raw RouterOS counts versus API-attributed counts.
5. Run Go race/tests/vet/build, frontend lint/build, and browser checks for all three entry paths.

Rollback is limited to the new IPv6 endpoint, derivation helper, detail projections, and frontend scope state; existing combined terminal data remains the compatibility path.
