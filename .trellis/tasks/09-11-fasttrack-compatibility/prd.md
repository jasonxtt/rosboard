# Shared FastTrack compatibility

## Requirements
Implement the user-approved simplified FastTrack design. Persisted RoutingRules, including disabled rules, share compatibility changes. Never restore until the last rule is deleted and owned routing objects are verified cleaned. Preserve externally changed/deleted rules. Show automatic adjustments and require explicit plan-bound risk acknowledgements for unsupported configurations. Never flush connections, alter comments/order, or adopt foreign filters.

## Acceptance
Regression coverage for analysis, stale dependencies, acknowledgements, write-ahead recovery, external divergence, multiple consumers, disable, last deletion, and communication failure. Standard Go/frontend checks pass. Production deployment and merge require separate acceptance; this task stays in progress.
