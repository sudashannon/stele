---
title: Markdown editor implementation research
tags:
  - markdown
  - editor
  - concurrency
  - deepwork
---

## Goal

Add a safe, source-first Markdown editing path while preserving `MarkdownViewer` as a read-only component. The editor must save only authorized existing artifacts and must not silently overwrite concurrent changes.

## Confirmed repository context

- `main.go:377-379` registers `/api/artifact` through `handleGetArtifact`; `main.go:728-767` resolves a workspace, checks `artifactPathAllowed`, and reads bytes. It currently has no write method dispatch.
- `main.go:482-510` provides the authoritative workspace resolution used by existing artifact reads.
- `artifactPathAllowed` is lexical-only for OpenSpec/Trellis/Docs and performs final symlink containment only for Superpowers. PUT therefore needs an additional resolved-path containment check for every workspace kind before writing.
- `main.go:37-60` embeds `web/dist`; deployment requires the frontend build before the Go build. The static entry document is already `no-cache` while fingerprinted assets are immutable.
- `web/src/components/MarkdownViewer.tsx` is the shared read-only document surface. `web/src/App.tsx:528-565` centralizes viewer construction across routes; `ReportView.tsx:371` uses `path=null` and must never receive edit controls.
- `web/src/api/client.ts:172-177` only exposes read-only `fetchArtifactContent` today. `web/package.json` has no editor framework dependency.
- `knowledge/2026-08-16-markdown-viewer-tibis.md` and `docs/superpowers/plans/2026-08-16-markdown-viewer-tibis.md` explicitly defer editing until workspace-authorized PUT, ETag/If-Match, atomic writes, failure recovery, and Markdown round-trip evidence exist.
- Existing atomic persistence patterns use same-directory temporary files and `os.Rename` (`internal/sessions/store.go`, `internal/todo/store.go`).

## Decisions for this implementation

1. Source-first editor built on a controlled textarea, not TipTap. This preserves exact Markdown, Front Matter, Mermaid, PlantUML, task lists, and malformed Markdown without introducing a serializer or a new dependency. CodeMirror can replace the editing surface later without changing the HTTP contract.
2. Only existing authorized text/Markdown artifacts are editable in this phase. No create, rename, move, delete, binary editing, or report editing.
3. `PUT /api/artifact` uses the same `path` and `workspace` resolution as GET, then verifies the final `EvalSymlinks` target remains inside the resolved workspace root for every workspace kind. It requires a strong, explicit `If-Match`; missing, wildcard, weak, or malformed validators fail rather than overwrite.
4. The current target bytes are read and hashed under the artifact write lock. The atomic rename targets the resolved file, not a lexical file-level symlink.
5. Writes are bounded, serialized per artifact, mode-preserving, same-directory atomic replacements. The original remains intact on validation or write failure.
6. `MarkdownViewer` gets an optional `onEdit` affordance. `ReportView` and `path=null` never pass it. `MarkdownEditor` is a separate component and owns dirty state, save status, conflict display, and retry/reload choices.

## Planned phases

- Research/review: review this plan and revise before code.
- Backend: method dispatch, ETag calculation, authorized PUT, bounded atomic write, contract tests.
- Editor: API client/types, source-first editor, App overlay integration, conflict/dirty/save tests.
- Verification: independent review after each phase, full Go/frontend tests, build, live service deployment, browser smoke.

## Risks and checks

- A handler-level global lock is simple and correct for this single-process service; if profiling later shows contention, narrow it to keyed locks.
- PUT must reject symlink targets that resolve outside the workspace for OpenSpec, Trellis, and Docs as well as Superpowers.
- Reject `If-Match: *`, weak tags, malformed tags, and any tag not equal to the current strong content ETag. Hash the current bytes while holding the write lock.
- Preserve `HEAD` as a read operation; only unsupported methods should return 405.
- Reject binary/non-UTF-8 content with 415 before exposing the edit affordance.
- Contract tests must cover symlink containment for non-Superpowers, wildcard validators, same-ETag concurrent PUT (one success/one 412), and write-failure preservation of bytes and mode.
