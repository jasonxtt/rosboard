# Frontend Development Guidelines

> Best practices for frontend development in this project.

---

## Overview

The frontend is a TypeScript React application built with Vite. `App.tsx`
owns the shell, selected device, navigation, theme, and shared refresh
preferences. Feature-specific screens and contracts live under
`web/src/features/`; shared display types and formatting live under
`web/src/lib/`.

The important frontend boundaries are explicit API parsing, device-scoped
server reads, local draft state for forms, and preserving the existing
embedded build (`web` → `internal/ui/dist`).

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Module organization and file layout | Active |
| [Component Guidelines](./component-guidelines.md) | Terminal filters, toolbar sizing, and text-link buttons | Active |
| [Hook Guidelines](./hook-guidelines.md) | Custom hooks, data fetching patterns | Active |
| [State Management](./state-management.md) | Local state, global state, server state | Active |
| [Quality Guidelines](./quality-guidelines.md) | Frontend verification and visual QA handoff | Active |
| [Type Safety](./type-safety.md) | Type patterns, validation | Active |

---

## How to Use These Guidelines

When adding frontend code:

1. Keep feature state and API contracts with the feature that owns them.
2. Parse network data at the API boundary; components should receive typed
   values and render states explicitly.
3. Use the existing React/Vite toolchain and shared formatting/types before
   adding a dependency.
4. Keep device changes and sensitive values out of unrelated persistent state.

The goal is to help AI assistants and new team members understand how YOUR project works.

---

**Language**: All documentation should be written in **English**.
