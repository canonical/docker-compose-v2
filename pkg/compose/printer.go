/*
   Copyright 2020 Docker Compose CLI authors

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

package compose

import (
	"fmt"
	"sync"

	"github.com/docker/compose/v2/pkg/api"
)

// logPrinter watch application containers an collect their logs
type logPrinter interface {
	HandleEvent(event api.ContainerEvent)
	Run(cascadeStop bool, exitCodeFrom string, stopFn func() error) (int, error)
	Cancel()
	Stop()
}

type printer struct {
	queue    chan api.ContainerEvent
	consumer api.LogConsumer
	stopCh   chan struct{} // stopCh is a signal channel for producers to stop sending events to the queue
	stop     sync.Once
}

// newLogPrinter builds a LogPrinter passing containers logs to LogConsumer
func newLogPrinter(consumer api.LogConsumer) logPrinter {
	printer := printer{
		consumer: consumer,
		queue:    make(chan api.ContainerEvent),
		stopCh:   make(chan struct{}),
		stop:     sync.Once{},
	}
	return &printer
}

func (p *printer) Cancel() {
	// note: HandleEvent is used to ensure this doesn't deadlock
	p.HandleEvent(api.ContainerEvent{Type: api.UserCancel})
}

func (p *printer) Stop() {
	p.stop.Do(func() {
		close(p.stopCh)
		for {
			select {
			case <-p.queue:
				// purge the queue to free producers goroutines
				// p.queue will be garbage collected
			default:
				return
			}
		}
	})
}

func (p *printer) HandleEvent(event api.ContainerEvent) {
	select {
	case <-p.stopCh:
		return
	default:
		p.queue <- event
	}
}

//nolint:gocyclo
func (p *printer) Run(cascadeStop bool, exitCodeFrom string, stopFn func() error) (int, error) {
	var (
		aborting bool
		exitCode int
	)
	defer p.Stop()

	containers := map[string]struct{}{}
	for {
		select {
		case <-p.stopCh:
			return exitCode, nil
		case event := <-p.queue:
			container, id := event.Container, event.ID
			switch event.Type {
			case api.UserCancel:
				aborting = true
			case api.ContainerEventAttach:
				if _, ok := containers[id]; ok {
					continue
				}
				containers[id] = struct{}{}
				p.consumer.Register(container)
			case api.ContainerEventExit, api.ContainerEventStopped, api.ContainerEventRecreated:
				if !event.Restarting {
					delete(containers, id)
				}
				if !aborting {
					p.consumer.Status(container, fmt.Sprintf("exited with code %d", event.ExitCode))
					if event.Type == api.ContainerEventRecreated {
						p.consumer.Status(container, "has been recreated")
					}
				}
				if cascadeStop {
					if !aborting {
						aborting = true
						err := stopFn()
						if err != nil {
							return 0, err
						}
					}
					if event.Type == api.ContainerEventExit {
						if exitCodeFrom == "" {
							exitCodeFrom = event.Service
						}
						if exitCodeFrom == event.Service {
							exitCode = event.ExitCode
						}
					}
				}
				if len(containers) == 0 {
					// Last container terminated, done
					return exitCode, nil
				}
			case api.ContainerEventLog:
				if !aborting {
					p.consumer.Log(container, event.Line)
				}
			case api.ContainerEventErr:
				if !aborting {
					p.consumer.Err(container, event.Line)
				}
			}
		}
	}
}
