# Repository Guidelines

## Project Structure & Module Organization

The Go proxy lives in `proxy/`. Its entry point is `proxy/cmd/proxy/main.go`; internal packages are grouped by responsibility under `proxy/internal/` (`handler`, `provider`, `service`, `config`, and `model`). Keep Go tests beside their source files as `*_test.go`.

The Remix/React dashboard lives in `web/`. Route modules are in `web/app/routes`, reusable UI in `web/app/components`, and protocol-normalization or formatting helpers in `web/app/utils`. Static assets belong in `web/public`. Root-level files provide configuration, Docker support, and shared development scripts. Generated files such as `requests.db`, `requests/`, `bin/`, and `web/build/` must remain untracked.

## Build, Test, and Development Commands

- `make install`: download Go modules and install npm dependencies.
- `make dev`: build and start the proxy on port 3001 and the dashboard on port 5173.
- `make run-proxy` / `make run-web`: run one service independently.
- `make build`: compile the Go binary and build the production web bundle.
- `cd proxy && go test ./...`: run all Go tests.
- `cd web && npm run typecheck && npm test`: run strict TypeScript checks and the aggregated frontend test suite.
- `cd web && npm run dev -- --port 5174`: use an alternate dashboard port when needed.

## Coding Style & Naming Conventions

Run `gofmt` on Go files. Use idiomatic Go package names and exported `PascalCase` identifiers. Frontend code follows Prettier: two spaces, 80-column lines, double quotes, semicolons, and trailing ES5 commas. Name React components `PascalCase`, utilities `camelCase`, and tests `*.test.ts` or `*.test.tsx`. Preserve exact model identifiers and protocol field names; do not normalize their casing for display.

## Testing Guidelines

Add regression tests for behavior changes. Protocol work must cover applicable Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses payloads, including streaming when relevant. There is no numeric coverage threshold; verify changed paths directly and run both backend and frontend suites before review.

## Commit & Pull Request Guidelines

Use standard Conventional Commits; never prefix subjects with `dsw-<number>`. Examples: `feat(sessions): show reasoning tokens` or `fix(proxy): preserve streaming usage`. Husky runs Prettier, type checking, frontend tests, and commitlint. Pull requests should explain user-visible behavior, list validation commands, link the relevant issue, and include screenshots for dashboard changes. Call out protocol or schema compatibility impacts explicitly.

## Security & Configuration

Copy `config.yaml.example` or `.env.example` for local setup. Never commit API keys, `config.yaml`, `.env`, or captured databases. Request headers and SQLite records may contain authorization credentials; treat exports and screenshots as sensitive.
