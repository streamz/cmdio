# cmdio
flexible cmd wrapper and io re-director

examples:

program.sh
```shell
#!/bin/bash

echo $1
echo "program.sh running as child of PID $$"
```

main.go

```go
package main

import (
    "fmt"
    "os"
    "os/user"
    
    c "github.com/streamz/cmdio"
)

func stdOptions() *c.Options {
    usr, _ := user.Current()
    return &c.Options{
        In:  os.Stdin,
        Out: os.Stdout,
        Err: os.Stderr,
        Usr: usr,
    }
}

// run the script synchronously
func sync() *c.Info {
    return c.New(stdOptions).Run("program.sh")
}

// run the script asynchronously
func async() *c.Info {
    _, ctx := c.New(stdOptions).Start("program.sh")
    info := <-ctx
    return &info
}

func main() {
    println(fmt.Sprintf("running sync, result: %+v", *sync()))
    println(fmt.Sprintf("running async, result: %+v", *async()))
}
```
