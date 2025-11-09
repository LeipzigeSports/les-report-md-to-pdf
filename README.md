This repository contains a Go application which exposes a simple HTTP server that is capable of transforming Markdown documents into PDFs.
Its primary purpose is to build reproducible PDFs from monthly team reports.

# Installation 

The application is shipped as a container image. 
It can be pulled with the following command.

```
docker pull ghcr.io/leipzigesports/les-report-md-to-pdf:0.1.0
```

The application asserts that a [Pandoc](https://pandoc.org/) and [Typst](https://typst.app/) installation is present.
The container image is based on [pandoc/typst](https://hub.docker.com/r/pandoc/typst) which meets both of these conditions.

# Configuration

The application is configured using command line arguments or environment variables.

| **Argument** | **Environment variable** | **Description** |
|:--|:--|:--|
| `--applicationRoot` | `APPLICATION_ROOT` | Path to directory containing application binary (default: [`os.Getwd()`](https://pkg.go.dev/os#Getwd)) |
| `--host` | `HTTP_HOST` | Host address for server (default: `0.0.0.0`) |
| `--pandocExecutable` | `PANDOC_EXECUTABLE` | Name of Pandoc executable (default: `pandoc`) |
| `--port` | `HTTP_PORT` | Port for server (default: `3333`) |

# Security

This application should **not** be exposed to the internet without access control.
It implements very limited security safeguards. 
Using HTTP Basic authentication is recommended at the very least.
For more advanced setups, a middleware like [OAuth2 Proxy](https://oauth2-proxy.github.io/oauth2-proxy/) may be suitable.

# License

Apache 2.0
