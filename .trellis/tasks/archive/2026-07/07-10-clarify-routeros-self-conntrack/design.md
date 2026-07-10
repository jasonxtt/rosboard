# RouterOS conntrack wording design

Extend the existing typed RouterOS connection model and terminal API projection with `seenReply` and `assured` booleans. The service already reads both raw fields, so no new RouterOS endpoint or database migration is required.

Keep all raw conntrack rows for Winbox parity. Presentation derives flags and labels:

- `S`: destination has replied (`seen-reply=true`);
- `A`: RouterOS marks the entry assured (`assured=true`);
- neither: tracked row without a reply/assurance.

The stable `routeros:self` terminal gets the explicit display name `RouterOS 本机连接跟踪`. Its list and detail summaries use tracking-entry wording and show replied/unreplied counts. Normal terminals continue to use the existing connection wording.

Runtime credentials remain outside the binary. Launch uses existing environment variables or a local ignored `config.yaml`; credentials must not be committed.
