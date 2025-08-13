/*
Copyright © 2020 streamz <bytecodenerd@gmail.com>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmdio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// Options configures how a command will be executed.
type Options struct {
	In  io.Reader   // Input stream (stdin)
	Out io.Writer   // Output stream (stdout)
	Err io.Writer   // Error stream (stderr)
	Env []string    // Environment variables
	Usr *user.User  // User to run command as
}

// Info contains information about a command's execution status and result.
type Info struct {
	Error    error
	RunT     time.Duration
	Pid      int
	Exit     int
	StartT   int64
	EndT     int64
	Finished bool
	Signaled bool
}

type status int

const (
	_uninitialized status = iota
	_exited
	_running
	_signaled
)

// CmdIo manages command execution with I/O redirection and process control.
// It provides both synchronous and asynchronous command execution with
// proper cleanup and signal handling.
type CmdIo struct {
	in  io.Reader
	out io.Writer
	err io.Writer
	env []string
	lok *sync.Mutex
	usr *user.User
	ini *sync.Once
	sta atomic.Int32  // status using atomic for thread safety
	inf Info
	str time.Time
	ech chan Info
	sch chan bool
	syn chan struct{}
	ncp noCopy
}

// New creates a new CmdIo instance with the provided options.
// If no user is specified in options, it defaults to the current user.
func New(optFn func() *Options) *CmdIo {
	opts := optFn()
	usr := opts.Usr
	if usr == nil {
		usr, _ = user.Current()
	}
	c := &CmdIo{
		in:  opts.In,
		out: opts.Out,
		err: opts.Err,
		env: opts.Env,
		usr: usr,
		lok: &sync.Mutex{},
		ini: &sync.Once{},
		inf: Info{Pid: 0, Exit: -1},
		ech: make(chan Info, 1),
		sch: make(chan bool, 1),
		syn: make(chan struct{}),
	}
	c.sta.Store(int32(_uninitialized))
	return c
}

// Start asynchronously starts a command and returns channels for monitoring.
// The first channel signals whether the command started successfully.
// The second channel receives the final execution info when the command completes.
func (c *CmdIo) Start(name string, args ...string) (<-chan bool, <-chan Info) {
	init := false
	c.ini.Do(func() {
		init = true
		go signalHandler()
		go c.runFn(name, args...)
	})
	if !init {
		c.ech <- Info{
			Error:    fmt.Errorf("already executed, cannot reuse CmdIo"),
			RunT:     0,
			Pid:      0,
			Exit:     0,
			StartT:   0,
			EndT:     0,
			Finished: true,
			Signaled: false,
		}
	}
	return c.sch, c.ech
}

// Run synchronously executes a command and waits for it to complete.
// It returns the execution info containing exit status and any errors.
func (c *CmdIo) Run(name string, args ...string) *Info {
	_, complete := c.Start(name, args...)
	info := <-complete
	return &info
}

// RunContext synchronously executes a command with context support.
// The command will be terminated if the context is cancelled.
// It returns the execution info containing exit status and any errors.
func (c *CmdIo) RunContext(ctx context.Context, name string, args ...string) *Info {
	started, complete := c.StartContext(ctx, name, args...)
	<-started
	info := <-complete
	return &info
}

// StartContext asynchronously starts a command with context support.
// The command will be terminated if the context is cancelled.
// Returns channels for monitoring start success and completion.
func (c *CmdIo) StartContext(ctx context.Context, name string, args ...string) (<-chan bool, <-chan Info) {
	started, complete := c.Start(name, args...)
	
	// Monitor context cancellation
	go func() {
		select {
		case <-ctx.Done():
			// Context cancelled, terminate the command
			_ = c.Terminate()
		case <-c.syn:
			// Command completed naturally
		}
	}()
	
	return started, complete
}

// Terminate sends a SIGTERM signal to the running command and all its children.
// Returns nil if the command is not running or has already finished.
func (c *CmdIo) Terminate() error {
	c.lok.Lock()
	defer c.lok.Unlock()

	if status(c.sta.Load()) == _uninitialized || c.inf.Finished {
		return nil
	}

	c.sta.Store(int32(_signaled))
	c.inf.Signaled = true
	return syscall.Kill(-c.inf.Pid, syscall.SIGTERM)
}

// Info returns a copy of the current execution state of a command.
// The runtime is updated if the command is still running.
func (c *CmdIo) Info() Info {
	c.lok.Lock()
	defer c.lok.Unlock()

	switch status(c.sta.Load()) {
	case _running:
		c.inf.RunT = time.Since(c.str)
	case _exited:
		c.inf.Finished = true
	case _signaled:
		// Already marked as signaled, runtime was captured at termination
	case _uninitialized:
		// Command not yet started
	}
	return c.inf
}

// Join returns a channel that is closed when the command completes.
// This can be used to wait for command completion in a select statement.
func (c *CmdIo) Join() <-chan struct{} {
	return c.syn
}

func (c *CmdIo) runFn(name string, args ...string) {
	defer func() {
		c.ech <- c.Info()
		close(c.syn)
	}()

	cmd := c.newCmd(name, args...)
	now := time.Now()
	if e := cmd.Start(); e != nil {
		c.complete(&now, e)
		c.sch <- false
		return
	}

	c.init(&now, cmd)
	c.sch <- true
	e := cmd.Wait()
	c.complete(&now, e)
}

func (c *CmdIo) newCmd(name string, args ...string) *exec.Cmd {
	uid, _ := strconv.Atoi(c.usr.Uid)
	gid, _ := strconv.Atoi(c.usr.Gid)

	cred := &syscall.Credential{
		Uid:         uint32(uid),
		Gid:         uint32(gid),
		NoSetGroups: true,
	}

	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = syscallAttrs(cred)

	// wire IO
	cmd.Stdin = os.Stdin
	if c.in != nil && c.in != os.Stdin {
		cmd.Stdin = c.in
	}
	cmd.Stdout = os.Stdout
	if c.out != nil && c.out != os.Stdout {
		cmd.Stdout = io.MultiWriter(c.out, os.Stdout)
	}
	cmd.Stderr = os.Stderr
	if c.err != nil && c.err != os.Stderr {
		cmd.Stderr = io.MultiWriter(c.err, os.Stderr)
	}

	cmd.Dir = os.Getenv("PWD")
	cmd.Env = os.Environ()
	if len(c.env) > 0 {
		cmd.Env = c.env
	}

	return cmd
}

func (c *CmdIo) init(t *time.Time, cmd *exec.Cmd) {
	c.lok.Lock()
	defer c.lok.Unlock()

	c.inf.Pid = cmd.Process.Pid
	c.inf.StartT = t.UnixNano()
	c.sta.Store(int32(_running))
}

func (c *CmdIo) complete(t *time.Time, err error) {
	code := 0
	if err != nil {
		code = exitErr(err)
	}
	c.endState(t, code, err)
}

func (c *CmdIo) endState(t *time.Time, code int, err error) {
	c.lok.Lock()
	defer c.lok.Unlock()

	c.inf.Error = err
	c.inf.Exit = code
	c.inf.StartT = t.UnixNano()
	c.inf.EndT = time.Now().UnixNano()
	if status(c.sta.Load()) != _signaled {
		c.inf.Finished = true
		c.sta.Store(int32(_exited))
	}
}

func exitErr(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if sig := ws.Signal(); sig > 0 {
				return int(sig)
			}
			return ws.ExitStatus()
		}
		// If type assertion fails, return a generic error code
		return 1
	}
	return 0
}
