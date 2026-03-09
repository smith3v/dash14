# Repository Guidelines

## Coding Style & Naming Conventions
Use Go 1.26 features sparingly and keep imports tidy via `go fmt` (or `gofmt` + `goimports` in your editor). Follow idiomatic Go naming: exported types and functions in PascalCase, unexported identifiers in camelCase, configuration structs named after their domain (`DatabaseConfig`, etc.). Prefer small, focused packages.

## Testing Guidelines
Table-driven tests in `_test.go` files colocated with the code are preferred. Target deterministic units: database repositories (using a test schema) and Telegram command handlers. Run `go test ./...` before pushing; add `-cover` to monitor coverage for new features. When adding fixtures, place them alongside tests or under a dedicated `testdata/` folder.

## Commit & Pull Request Guidelines
Commit with a clear commit messages. Follow that tone, keep lines ≤72 characters, and describe *why* the change matters in the body. Pull requests should include: a short summary, configuration or migration notes, linked issues, and screenshots or logs when behavior changes.

## Security & Configuration Tips
Never commit real tokens or database passwords; `config.json` is ignored—use `config.example.json` as your template and inject secrets via environment variables or local-only files. Review rate limits and bot permissions before enabling new Telegram commands. Rotate database credentials and bot tokens if they leak, and prefer least-privilege roles for the PostgreSQL user.
