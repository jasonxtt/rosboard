# Existing behavior and feasibility

Research baseline: main d48f5e7. Preparation branch base: origin/main 0d8c670; the intervening change updates Docker documentation only. No production instance or RouterOS was accessed during this research.

## Evidence

- internal/api/routing_rules.go listPolicyRoutingRules reads the device repository; internal/store/routing_rule.go ListRoutingRules queries local policy_v2_routing_rules.
- internal/api/server.go creates a new device UUID on add; archive retains the record, while permanent purge removes the configuration record.
- internal/store/installation.go persists manager_instance_id; internal/ownership/identity.go hashes manager/device scope and logical object identity into rbs_<scope8>_<object8>. The hash does not contain a reversible policy definition.
- internal/policyv2/reconcile.go ScanManaged marks foreign scopes; DiffDesired skips foreign objects. Current-scope objects absent from desired state can become stale cleanup candidates. Merely rewriting ownership comments is therefore not a safe import.
- internal/policyv2/discovery.go currently returns an empty ExistingPolicy list. The plan summary's Adopt field is not an implemented importer.
- internal/policyv2/routing_desired.go compiles relationships into address lists, marks, tables, routes and DNS projections; shared execution groups prevent a one-object/one-policy assumption.
- internal/accesscontrol/desired.go emits per-family target jumps/chains or internet filters and address memberships. internal/accesscontrol/schedule.go defines representable weekly schedules.
- internal/policyv2/proposal.go and access_proposal.go keep previews read-only. Existing proposals are narrow, single-rule bundles; whole-scope adoption needs a bounded batch transaction rather than a loop that exposes partially saved graphs.
- internal/policyv2/manager.go provides write gates, staged apply, activation, read-back, jobs and separate domain commits. Its normal cleanup must not gain broad foreign-scope privileges.
- internal/api/provisioning.go provisioningCleanupScript deletes only the dedicated RouterOS account/group, not policy rules.

## Completed investigation tests

A temporary Go overlay (outside the repository) created an old-device RoutingRule/Egress/TargetList, called PurgeDevice, and opened a new device. The old policy persisted, including across close/reopen; the new device listed zero rules. Existing selected cross-manager/scoped-ownership and legacy-scan tests also passed. This establishes current list/ownership behavior, not implementation of adoption.

PurgeDevice does not call purgePolicyData in the researched baseline. That cleanup omission is explicitly outside this task and must not be used as an adoption dependency.

## Limits that shaped the plan

RouterOS exposes actual configuration, not subscription URLs, original policy IDs, or terminal auto-follow intent. Import current content as manual/fixed snapshots. Parse only known generator shapes; decline ambiguous graphs. The user accepted controlled replacement and whole-scope selection, avoiding permanent object aliases and per-rule dependency splitting.

Reference: MikroTik REST API supports reading existing menu objects and modifying resources: https://help.mikrotik.com/docs/spaces/ROS/pages/47579162/REST%2BAPI
