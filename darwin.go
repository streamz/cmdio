//go:build darwin
// +build darwin

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
	"encoding/binary"
	"os"
	"os/signal"
	"syscall"
	"unsafe"
)

// Copyright (c) 2014 Mitchell Hashimoto
const (
	ctrlKern = 1
	kernProc = 14
	kernProcAll = 0
	kinfoStructSize = 648
)

type kinfoProc struct {
	_    [40]byte
	Pid  int32
	_    [199]byte
	Comm [16]byte
	_    [301]byte
	PPid int32
	_    [84]byte
}

func darwinSyscall() (*bytes.Buffer, error) {
	mib := [4]int32{ctrlKern, kernProc, kernProcAll, 0}
	size := uintptr(0)

	_, _, errno := syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])),
		4,
		0,
		uintptr(unsafe.Pointer(&size)),
		0,
		0)

	if errno != 0 {
		return nil, errno
	}

	bs := make([]byte, size)
	_, _, errno = syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])),
		4,
		uintptr(unsafe.Pointer(&bs[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
		0)

	if errno != 0 {
		return nil, errno
	}

	return bytes.NewBuffer(bs[0:size]), nil
}

func children(ppid int) ([]int, error) {
	buf, err := darwinSyscall()
	if err != nil {
		return nil, err
	}

	procs := make([]*kinfoProc, 0, 50)
	k := 0
	for i := kinfoStructSize; i < buf.Len(); i += kinfoStructSize {
		proc := &kinfoProc{}
		err = binary.Read(bytes.NewBuffer(buf.Bytes()[k:i]), binary.LittleEndian, proc)
		if err != nil {
			return nil, err
		}

		k = i
		procs = append(procs, proc)
	}

	var pids []int
	for _, p := range procs {
		if ppid == int(p.PPid) {
			pids = append(pids, int(p.Pid))
		}
	}
	return pids, nil
}

// end Copyright (c) 2014 Mitchell Hashimoto


func syscallAttrs(cred *syscall.Credential) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Credential: cred,
		Setsid:     true,
	}
}

func signalHandler() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, os.Kill)
	<-c
	killChildren()
}

func killChildren() {
	// work around for Darwin's lack of Pdeathsig support
	// Pdeathsig: syscall.SIGKILL,
	ch, e := children(os.Getpid())
	if e == nil {
		for _, pid := range ch {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}




