# lambda-bridge

A Go project set up with Claude Code and modern development tools.

## Structure

```
.
├── cmd/           # Application entrypoints
├── internal/      # Private application code
├── pkg/           # Public libraries
├── api/           # API definitions
├── configs/       # Configuration files
├── scripts/       # Build and utility scripts
├── docs/          # Documentation
└── tests/         # Integration tests
```

## Development

### Prerequisites
- Go 1.21+
- Claude Code
- Docker (optional)

### Setup
```bash
# Install dependencies
go mod download

# Install dev tools
make install-tools

# Run with hot reload
make air
# or
task dev
```

### Testing
```bash
make test
# or
task test
```

### Linting
```bash
make lint
# or  
task lint
```

## Claude Code Usage

Start Claude Code in this project:
```bash
claude
```

Use Plan Mode for complex features:
```bash
claude --plan "Design a REST API for user management"
```
