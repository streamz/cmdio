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
	"bytes"
	"context"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var (
	_, b, _, _ = runtime.Caller(0)
	Testdata   = filepath.Dir(b) + "/testdata/"
)

type stringReader struct {
	data []string
	step int
}

func (r stringReader) Read(p []byte) (n int, err error) {
	if r.step < len(r.data) {
		s := r.data[r.step]
		n = copy(p, s)
		r.step++
	} else {
		err = io.EOF
	}
	return
}

func stdOptions() *Options {
	usr, _ := user.Current()
	return &Options{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
		Env: append([]string{"CMDIO_TEST=running test"}, os.Environ()...),
		Usr: usr,
	}
}

func bufOptions(in io.Reader, out, err io.Writer) func() *Options {
	usr, _ := user.Current()
	return func() *Options {
		return &Options{
			In:  in,
			Out: out,
			Err: err,
			Usr: usr,
		}
	}
}

func run(t *testing.T, script string, optFn func() *Options) *Info {
	t.Helper()
	return New(optFn).Run(Testdata + script)
}

func start(t *testing.T, script string, optFn func() *Options) *Info {
	t.Helper()
	_, ctx := New(optFn).Start(Testdata + script)
	info := <-ctx
	return &info
}

func terminate(t *testing.T, script string, optFn func() *Options) (*Info, error) {
	t.Helper()
	cmd := New(optFn)
	started, ctx := cmd.Start(Testdata + script)
	<-started

	time.Sleep(time.Second)

	// terminate the process
	err := cmd.Terminate()
	info := <-ctx
	return &info, err
}

func TestRun(t *testing.T) {
	t.Run("basic_run", func(t *testing.T) {
		info := run(t, "program.sh", stdOptions)
		assert.NoError(t, info.Error)
		assert.Equal(t, 0, info.Exit)
		assert.True(t, info.Finished)
	})
}

func TestReRun(t *testing.T) {
	t.Run("cannot_reuse_cmdio", func(t *testing.T) {
		cmd := New(stdOptions)
		info := cmd.Run(Testdata + "program.sh")
		assert.NoError(t, info.Error)
		info = cmd.Run(Testdata + "program.sh")
		assert.Error(t, info.Error)
		assert.Contains(t, info.Error.Error(), "cannot reuse CmdIo")
	})
}

func TestStart(t *testing.T) {
	t.Run("async_start", func(t *testing.T) {
		info := start(t, "program.sh", stdOptions)
		assertStart(t, info)
	})
}

func TestTerminate(t *testing.T) {
	t.Run("terminate_running", func(t *testing.T) {
		info, err := terminate(t, "service.sh", stdOptions)
		assert.NoError(t, err)
		assertTerminate(t, info)
	})
}

func TestStartBufIO(t *testing.T) {
	t.Run("io_redirection", func(t *testing.T) {
		expected := "hello world\n"
		in := stringReader{
			step: 0,
			data: []string{expected},
		}
		out := bytes.NewBufferString("")
		err := bytes.NewBufferString("")
		cmd := New(bufOptions(in, out, err))
		info := cmd.Run(Testdata+"io.sh", "-")
		assert.NoError(t, info.Error)
		assert.Equal(t, expected, out.String())
		assert.Equal(t, expected, err.String())
	})
}

func assertTerminate(t *testing.T, info *Info) {
	assert.Error(t, info.Error)
	assert.False(t, info.Finished, "info should not be finished")
	assert.True(t, info.Signaled, "info should be Signaled")
	assert.Equal(
		t,
		15,
		info.Exit,
		"should exit with 15")
}

func assertStart(t *testing.T, info *Info) {
	assert.NoError(t, info.Error)
	assert.True(t, info.Finished, "info should be finished")
	assert.False(t, info.Signaled, "info should not be Signaled")
	assert.Equal(
		t,
		0,
		info.Exit,
		"should exit with 0")
}

func TestRunContext(t *testing.T) {
	t.Run("context_cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		
		cmd := New(stdOptions)
		info := cmd.RunContext(ctx, Testdata+"service.sh")
		
		// Command should be terminated due to context timeout
		assert.True(t, info.Signaled || info.Error != nil)
	})
	
	t.Run("context_not_cancelled", func(t *testing.T) {
		ctx := context.Background()
		cmd := New(stdOptions)
		info := cmd.RunContext(ctx, Testdata+"program.sh")
		
		assert.NoError(t, info.Error)
		assert.Equal(t, 0, info.Exit)
		assert.True(t, info.Finished)
	})
}

func TestStartContext(t *testing.T) {
	t.Run("start_with_context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		
		cmd := New(stdOptions)
		started, complete := cmd.StartContext(ctx, Testdata+"service.sh")
		
		<-started
		info := <-complete
		
		// Command should be terminated due to context timeout
		assert.True(t, info.Signaled || info.Error != nil)
	})
}

// Benchmarks
func BenchmarkRun(b *testing.B) {
	for i := 0; i < b.N; i++ {
		cmd := New(stdOptions)
		_ = cmd.Run(Testdata + "program.sh")
	}
}

func BenchmarkStart(b *testing.B) {
	for i := 0; i < b.N; i++ {
		cmd := New(stdOptions)
		_, complete := cmd.Start(Testdata + "program.sh")
		<-complete
	}
}

func BenchmarkRunContext(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		cmd := New(stdOptions)
		_ = cmd.RunContext(ctx, Testdata+"program.sh")
	}
}
