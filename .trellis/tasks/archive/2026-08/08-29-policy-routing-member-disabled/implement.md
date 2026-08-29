# Implementation plan

1. Add the source-save response/job contract and eligible-source automatic
   synchronization on the backend.
2. Batch automatic scheduled refresh application and add API/manager regression
   coverage for assigned versus unassigned sources.
3. Update the frontend source editor and wizard to wait for or trigger automatic
   synchronization, remove normal “待应用” wording, and keep exceptional plan
   handling.
4. Run Go tests/race/vet, frontend lint/build/audit, and diff checks.
5. Build and deploy through the repository acceptance gate, preserve the NAS
   rollback backup, verify service/API/assets, then wait for user inspection
   before committing.
