// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build windows

package main

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
)

var elog debug.Log

type myservice struct{}

func (m *myservice) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPauseAndContinue | svc.AcceptPreShutdown
	changes <- svc.Status{State: svc.StartPending}
	fasttick := time.Tick(500 * time.Millisecond)
	slowtick := time.Tick(2 * time.Second)
	tick := fasttick
	shutdown := make(chan struct{})
	checkpoint := uint32(1)
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
loop:
	for {
		select {
		case <-shutdown:
			break loop
		case <-tick:
			beep()
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
				// testing deadlock from https://code.google.com/p/winsvc/issues/detail?id=4
				time.Sleep(100 * time.Millisecond)
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				break loop
			case svc.PreShutdown:
				go func() {
					defer close(shutdown)
					elog.Info(1, "Preshutdown")
					// This for loop represents some long running process that
					// allows for periodically updating the service state so the
					// SCM doesn't think the process has become non-responsive
					for {
						// This sends a status update, with an increased
						// checkpoint so the SCM doesn't think progress has
						// stalled and a wait hint larger than the expected
						// time till the next update (in this case our fake
						// 'work' process is a time.Sleep).
						//
						// After 30 seconds of 'work', we return, closing the
						// shutdown channel, which breaks the loop, which allows
						// the server to fallthrough to the Stopped state after
						// the Execute method returns.
						changes <- svc.Status{State: svc.StopPending, WaitHint: 3000, CheckPoint: checkpoint}
						time.Sleep(1 * time.Second)
						checkpoint++
						if checkpoint > 30 {
							elog.Info(1, "Preshutdown keep alive end")
							return
						}
						elog.Info(1, "Preshutdown tick")
					}
				}()
			case svc.Pause:
				changes <- svc.Status{State: svc.Paused, Accepts: cmdsAccepted}
				tick = slowtick
			case svc.Continue:
				changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
				tick = fasttick
			default:
				elog.Error(1, fmt.Sprintf("unexpected control request #%d", c))
			}
		}
	}
	changes <- svc.Status{State: svc.StopPending, CheckPoint: checkpoint}
	return
}

func runService(name string, isDebug bool) {
	var err error
	if isDebug {
		elog = debug.New(name)
	} else {
		elog, err = eventlog.Open(name)
		if err != nil {
			return
		}
	}
	defer elog.Close()

	elog.Info(1, fmt.Sprintf("starting %s service", name))
	run := svc.Run
	if isDebug {
		run = debug.Run
	}
	err = run(name, &myservice{})
	if err != nil {
		elog.Error(1, fmt.Sprintf("%s service failed: %v", name, err))
		return
	}
	elog.Info(1, fmt.Sprintf("%s service stopped", name))
}
