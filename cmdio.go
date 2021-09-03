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
	"errors"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

type Options struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	Env []string
	Usr *user.User
}

// Info -
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

// CmdIo -
type CmdIo struct {
	in  io.Reader
	out io.Writer
	err io.Writer
	env []string
	lok *sync.Mutex
	usr *user.User
	ini *sync.Once
	sta status
	inf Info
	str time.Time
	ech chan Info
	sch chan bool
	syn chan struct{}
	ncp noCopy
}

// New - creates a new CmdIo
func New(optFn func() *Options) *CmdIo {
	opts := optFn()
	usr := opts.Usr
	if usr == nil {
		usr, _ = user.Current()
	}
	return &CmdIo{
		in:  opts.In,
		out: opts.Out,
		err: opts.Err,
		env: opts.Env,
		usr: usr,
		lok: &sync.Mutex{},
		ini: &sync.Once{},
		inf: Info{Pid: 0, Exit: -1},
		sta: _uninitialized,
		ech: make(chan Info, 1),
		sch: make(chan bool, 1),
		syn: make(chan struct{}),
	}
}

// Start - asynchronously starts a command
func (c *CmdIo) Start(name string, args ...string) (<-chan bool, <-chan Info) {
	init := false
	c.ini.Do(func() {
		init = true
		go signalHandler()
		go c.runFn(name, args...)
	})
	if !init {
		c.ech <- Info{
			Error:    errors.New("already executed, can not reuse CmdIo"),
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

// Run - synchronously runs a command
func (c *CmdIo) Run(name string, args ...string) *Info {
	_, complete := c.Start(name, args...)
	info := <-complete
	return &info
}

// Terminate - kills a command
func (c *CmdIo) Terminate() error {
	c.lok.Lock()
	defer c.lok.Unlock()

	if c.sta == _uninitialized || c.inf.Finished {
		return nil
	}

	c.sta = _signaled
	c.inf.Signaled = true
	return syscall.Kill(-c.inf.Pid, syscall.SIGTERM)
}

// Info - returns a copy of the current state of a command
func (c *CmdIo) Info() Info {
	c.lok.Lock()
	defer c.lok.Unlock()

	switch c.sta {
	case _running:
		c.inf.RunT = time.Now().Sub(c.str)
	case _exited:
		c.inf.Finished = true
	}
	return c.inf
}

// Join -
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
	c.sta = _running
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
	if c.sta != _signaled {
		c.inf.Finished = true
		c.sta = _exited
	}
}

func exitErr(err error) int {
	if e, ok := err.(*exec.ExitError); ok {
		ws := e.Sys().(syscall.WaitStatus)
		if sig := ws.Signal(); sig > 0 {
			return int(sig)
		}
		return ws.ExitStatus()
	}
	return 0
}
