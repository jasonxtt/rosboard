# Archive project on GitHub

## Goal

Publish the current local `rosboard` project to `https://github.com/jasonxtt/rosboard` as a clean, usable source-code archive whose GitHub landing page explains the project, its scope, setup, build, and deployment.

## Background

- The local repository is on `main`, has an established implementation history, and had a clean working tree before this Trellis task was created.
- The local repository has no configured Git remote.
- The target GitHub repository is public and its `main` branch contains one unrelated placeholder commit whose only file is a one-line `README.md` (`# rosboard`).
- GitHub CLI is installed but is not authenticated. HTTPS Git credentials may still be available independently and must be verified before publication.
- Local runtime data, the Go binary, frontend dependencies, and `configs/config.local.yaml` are ignored. The frontend distribution under `internal/ui/dist` is intentionally tracked because it is embedded into the Go binary. The tracked example configuration contains placeholders rather than live credentials.

## Requirements

- Rewrite the root README as a conventional project landing page based only on behavior confirmed in the repository.
- Use Chinese as the primary README language while retaining standard English technical names and commands.
- Cover the project purpose, main features, architecture/technology stack, prerequisites, RouterOS preparation, configuration, local development, production build/run, systemd deployment, repository layout, current limitations, and security notes.
- Keep documentation concise and operationally accurate; do not claim unsupported features or invent project status.
- Preserve the existing local source history and publish only intended tracked project/task changes.
- Run a tracked-file secret check and ensure ignored local configuration, SQLite data, the local Go binary, and dependency directories are not included; retain the intentionally tracked embedded frontend distribution.
- Run the repository's relevant backend and frontend checks before publication.
- Configure the supplied GitHub repository as `origin`, publish the resulting local `main`, and verify the remote commit and rendered README.
- Replace the target repository's one-commit placeholder history with the complete local `main` history using `--force-with-lease`.

## Acceptance Criteria

- [x] The root README provides an accurate, conventional GitHub overview and copy-pasteable setup/build/deployment commands.
- [x] No real credential, local config, runtime database, dependency directory, or local binary is tracked or staged.
- [x] Backend tests and frontend lint/build checks pass.
- [x] The intended files are committed with a clear commit message.
- [x] `origin` points to `https://github.com/jasonxtt/rosboard.git`.
- [x] GitHub `main` resolves to the new archive commit and displays the improved README.

## Out of Scope

- Product feature changes or refactoring.
- Docker packaging, release binaries, tags, CI/CD, screenshots, badges that depend on services not already configured, or repository settings such as topics and descriptions.
- Publishing local credentials, runtime data, or generated dependency directories.
