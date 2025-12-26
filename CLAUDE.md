# Claude Code Guidelines for Gale

## Code Style

- **DO NOT ADD COMMENTS UNLESS ABSOLUTELY NECESSARY** - Prefer self-documenting code with clear naming
- Only add comments for complex business logic that cannot be made obvious through code structure
- Remove redundant comments that merely restate what the code does

## Testing

- Use interfaces and dependency injection for testability
- All new code should have corresponding unit tests
- Use mock implementations from `internal/*/mock.go` for testing
- Integration tests requiring external services should use `//go:build integration` tag

## Architecture

- Follow clean architecture with dependency injection
- External dependencies (Docker, GitHub API) should be behind interfaces
- Use `Options` structs to allow injecting mock clients for testing
