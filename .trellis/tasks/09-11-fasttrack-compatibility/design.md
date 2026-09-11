# Design

Reuse DeviceWriteGate, existing Plan/acknowledgement and domain apply flow. A dedicated device-local journal records original/intended rule fields and mutation status. Fingerprints exclude runtime fields; comparisons are optimistic checks, not atomic RouterOS CAS. Unexpected read-back is never adopted as the last applied state. Detected divergence is sticky for the current holding period.

V1 handles no-mark, exact distinct marks and simple forward FastTrack without foreign producers. Complex matchers require acknowledgement. Analyze both IPv4 and IPv6 menus conservatively; no packet-space theorem prover. Hash filter and foreign mangle dependencies plus journal into the plan. Check again under the existing device gate before proposal commit and before mutation. Persisted proposals survive apply failure, so compatible changes are retained for retry.

Apply verified adjustments before routing staging/activation. Release only after successful full routing verification with no persisted consumers and no owned routing mangle. Pending release is retried under the gate with a fresh safety check. Mutations use the typed RouterOS client and exact ID, including a narrow unset primitive. Device-local journal retains outcome messages for subsequent previews. No generic lease framework or extra mutex.
