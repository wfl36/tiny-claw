# tiny-claw

A minimal AI coding agent built with Go.

## Architecture

- `cmd/claw/` - Program entry point
- `internal/engine/` - MainLoop core implementation
- `internal/provider/` - LLM interface abstraction and vendor SDK implementations
- `internal/context/` - Token monitoring, dynamic prompt assembly
- `internal/tools/` - Tool registry, middleware, basic tools (bash/edit etc.)
- `internal/memory/` - File-system based memory state storage
- `internal/feishu/` - Feishu bot interaction callbacks

## Quick Start

```bash
go run ./cmd/claw
```

## License

MIT
