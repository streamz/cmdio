# cmdio

[![Go Reference](https://pkg.go.dev/badge/github.com/streamz/cmdio.svg)](https://pkg.go.dev/github.com/streamz/cmdio)
[![Go Report Card](https://goreportcard.com/badge/github.com/streamz/cmdio)](https://goreportcard.com/report/github.com/streamz/cmdio)

A flexible command execution wrapper with I/O redirection for Go. Execute external commands with full control over stdin, stdout, stderr, and process lifecycle.

## Features

- **Synchronous and Asynchronous Execution** - Run commands blocking or non-blocking
- **Context Support** - Cancel commands using Go contexts (Go 1.23+)
- **I/O Redirection** - Full control over stdin, stdout, and stderr
- **Process Management** - Signal handling, termination, and process information
- **Thread-Safe** - Safe for concurrent use with atomic operations
- **Cross-Platform** - Works on Linux and Darwin (macOS)

## Requirements

- Go 1.23 or higher

## Installation

```bash
go get github.com/streamz/cmdio
```

## Usage

### Basic Example

```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/user"
    "time"
    
    "github.com/streamz/cmdio"
)

func main() {
    // Create options for command execution
    opts := func() *cmdio.Options {
        usr, _ := user.Current()
        return &cmdio.Options{
            In:  os.Stdin,
            Out: os.Stdout,
            Err: os.Stderr,
            Usr: usr,
        }
    }

    // Synchronous execution
    cmd := cmdio.New(opts)
    info := cmd.Run("ls", "-la")
    fmt.Printf("Exit code: %d\n", info.Exit)

    // Asynchronous execution
    cmd2 := cmdio.New(opts)
    started, complete := cmd2.Start("sleep", "5")
    if <-started {
        fmt.Println("Command started successfully")
        info := <-complete
        fmt.Printf("Command finished: %+v\n", info)
    }

    // Context-based execution with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    
    cmd3 := cmdio.New(opts)
    info = cmd3.RunContext(ctx, "sleep", "10")
    if info.Signaled {
        fmt.Println("Command was terminated due to timeout")
    }
}
```

### I/O Redirection

```go
package main

import (
    "bytes"
    "fmt"
    "os/user"
    
    "github.com/streamz/cmdio"
)

func main() {
    // Capture command output
    var stdout, stderr bytes.Buffer
    
    opts := func() *cmdio.Options {
        usr, _ := user.Current()
        return &cmdio.Options{
            In:  nil,
            Out: &stdout,
            Err: &stderr,
            Usr: usr,
        }
    }
    
    cmd := cmdio.New(opts)
    info := cmd.Run("echo", "Hello, World!")
    
    fmt.Printf("Output: %s", stdout.String())
    fmt.Printf("Errors: %s", stderr.String())
    fmt.Printf("Exit code: %d\n", info.Exit)
}
```

### Process Termination

```go
package main

import (
    "time"
    "os"
    "os/user"
    
    "github.com/streamz/cmdio"
)

func main() {
    opts := func() *cmdio.Options {
        usr, _ := user.Current()
        return &cmdio.Options{
            In:  os.Stdin,
            Out: os.Stdout,
            Err: os.Stderr,
            Usr: usr,
        }
    }
    
    cmd := cmdio.New(opts)
    started, complete := cmd.Start("sleep", "60")
    
    if <-started {
        // Let it run for 2 seconds
        time.Sleep(2 * time.Second)
        
        // Terminate the command
        err := cmd.Terminate()
        if err != nil {
            panic(err)
        }
        
        info := <-complete
        if info.Signaled {
            fmt.Println("Command was successfully terminated")
        }
    }
}
```

## API Documentation

Full API documentation is available at [pkg.go.dev](https://pkg.go.dev/github.com/streamz/cmdio).

## Platform-Specific Behavior

### Darwin (macOS)
- Supports process group management
- Forwards signals to child processes
- Uses BSD-specific system calls for process enumeration

### Linux
- Supports process death signal (Pdeathsig)
- Automatic cleanup when parent dies

## Testing

Run tests with coverage:
```bash
go test -cover ./...
```

Run benchmarks:
```bash
go test -bench=. -benchmem ./...
```

## License

Apache License 2.0 - See LICENSE file for details
