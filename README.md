# OpenAPI CLI Generator

[![GoDoc](https://godoc.org/github.com/valknarthing/openapi-cli-generator?status.svg)](https://godoc.org/github.com/valknarthing/openapi-cli-generator)
[![Go Report Card](https://goreportcard.com/badge/github.com/valknarthing/openapi-cli-generator)](https://goreportcard.com/report/github.com/valknarthing/openapi-cli-generator)
[![Platforms](https://img.shields.io/badge/platform-win%20%7C%20mac%20%7C%20linux-ligh.svg)](https://github.com/valknarthing/openapi-cli-generator/releases)

<img alt="openapi-to-cli" src="https://user-images.githubusercontent.com/106826/46594546-a8bb2480-ca88-11e8-90ec-fb87e51009a8.png">

---

**Note: this project has been modernized to Go 1.24 and refactored to generate self-contained CLI artifacts.**

---

This project generates powerful, ready-to-use CLIs from OpenAPI 3 specifications. Unlike other generators, the resulting CLI is **self-contained**: all necessary support code (HTTP, auth, formatting) is bundled directly into your project's `internal/` directory, removing any dependency on this generator library.

## Features

- **Zero-Dependency Artifacts**: Bundles all support code in `internal/`.
- **Automatic Documentation**: Generates a comprehensive `README.md` for your new CLI.
- **Authentication**: API keys and OAuth2 (including Auth0 and PKCE support).
- **Modern CLI Primitives**: Powered by [Cobra](https://github.com/spf13/cobra) and [Viper](https://github.com/spf13/viper).
- **Flexible Input**: `stdin` or [CLI shorthand](https://github.com/valknarthing/openapi-cli-generator/tree/master/shorthand).
- **Advanced Filtering**: Integrated [JMESPath](http://jmespath.org/) for response projection.
- **Pretty Output**: Colored output via [Chroma](https://github.com/alecthomas/chroma) and structured logging via [zerolog](https://github.com/rs/zerolog).

## Getting Started

### 1. Install the Generator

```sh
$ GOPROXY=direct go install github.com/valknarthing/openapi-cli-generator@latest
```

### 2. Create Your CLI Project

```sh
# Set up your new project directory
$ mkdir my-api-cli && cd my-api-cli

# Initialize your Go module
$ go mod init my-api-cli

# Initialize the generator structure (bundles internal/ support code)
$ openapi-cli-generator init my-api-cli

# Generate the API commands and README.md
$ openapi-cli-generator generate openapi.yaml
```

### 3. Build and Install

```sh
# Fetch external dependencies
$ go mod tidy

# Build & install your new CLI
$ go install .

# Start using it!
$ my-api-cli --help
```

## OpenAPI Extensions

Change the behavior of your generated CLI using these extensions:

| Name                | Description                                                        |
| ------------------- | ------------------------------------------------------------------ |
| `x-cli-aliases`     | Sets up command aliases for operations.                            |
| `x-cli-description` | Provide an alternate description for the CLI.                      |
| `x-cli-ignore`      | Ignore this path, operation, or parameter.                         |
| `x-cli-hidden`      | Hide this path, or operation.                                      |
| `x-cli-name`        | Provide an alternate name for the CLI.                             |
| `x-cli-waiters`     | Generate commands/params to wait until a certain state is reached. |

## Customization

Since all support code is now in your `internal/` folder, you have full control over the generated CLI. Your `main.go` is the entrypoint where you can:
- Add custom global flags.
- Register HTTP middleware.
- Add pre-run/post-run logic.

See the generated `README.md` in your new project for specific details on how to use your new CLI.

## Development

This project uses Go 1.24+ features like `go:embed` for asset management.

```sh
# Build the generator locally
$ go build -o openapi-cli-generator main.go

# Run tests
$ ./test.sh
```

## License

MIT - See [LICENSE](LICENSE) for details.
