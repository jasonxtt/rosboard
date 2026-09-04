# Backend Development Guidelines

> Best practices for backend development in this project.

---

## Overview

rosboard is a small Go application. The executable in `cmd/rosboard` wires
configuration, SQLite stores, monitoring services, policy routing, and the
embedded web UI. Runtime code is kept under `internal/` so the package
boundaries below are enforced by the Go toolchain.

The most important backend rules are to preserve device isolation, keep
RouterOS writes behind the typed client and policy reconciler, avoid exposing
credentials, and return stable API error shapes.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Module organization and file layout | Active |
| [Database Guidelines](./database-guidelines.md) | SQLite terminal identity merge contracts | Active |
| [Authentication and Onboarding](./authentication-onboarding.md) | Administrator, session, setup phase, RouterOS verification, and reset contracts | Active |
| [Error Handling](./error-handling.md) | Error types, handling strategies | Active |
| [Quality Guidelines](./quality-guidelines.md) | Code standards, forbidden patterns | Active |
| [Logging Guidelines](./logging-guidelines.md) | Logging and secret boundaries | Active |
| [Monitoring Contracts](./monitoring-contracts.md) | RouterOS self attribution and IP-family terminal payloads | Active |
| [Policy Routing](./policy-routing.md) | Desired-state RouterOS policy lifecycle and ownership contracts | Active |
| [Runtime Configuration](./runtime-configuration.md) | Secure local YAML startup and delivery contracts | Active |

---

## How to Use These Guidelines

When adding backend code:

1. Keep new code in the package that owns the behavior.
2. Follow the device, RouterOS ownership, and credential boundaries before
   optimizing for convenience.
3. Add a focused regression test for a new contract or previously observed
   failure.
4. Update these documents only when a stable convention or security boundary
   changes; do not copy temporary feature implementation details here.

The goal is to help AI assistants and new team members understand how YOUR project works.

---

**Language**: All documentation should be written in **English**.
