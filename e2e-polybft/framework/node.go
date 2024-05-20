package framework

import (
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"time"
)

const (
	numberOfInterruptsOnForceStop       = 5
	shutdownGracePeriodOnForceStopInSec = 10
)

type Node struct {
	shuttingDown    atomic.Bool
	cmd             *exec.Cmd
	doneCh          chan struct{}
	exitResult      *exitResult
	shouldForceStop bool
}

func NewNode(binary string, args []string, stdout io.Writer) (*Node, error) {
	cmd := exec.Command(binary, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stdout

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	n := &Node{
		cmd:    cmd,
		doneCh: make(chan struct{}),
	}
	go n.run()

	return n, nil
}

func (n *Node) SetShouldForceStop(shouldForceStop bool) {
	n.shouldForceStop = shouldForceStop
}

func (n *Node) ExitResult() *exitResult {
	return n.exitResult
}

func (n *Node) Wait() <-chan struct{} {
	return n.doneCh
}

func (n *Node) run() {
	err := n.cmd.Wait()

	n.exitResult = &exitResult{
		Signaled: n.IsShuttingDown(),
		Err:      err,
	}
	close(n.doneCh)
	n.cmd = nil
}

func (n *Node) IsShuttingDown() bool {
	return n.shuttingDown.Load()
}

func (n *Node) Stop() error {
	if n.cmd == nil {
		// the server is already stopped
		return nil
	}

	if err := n.cmd.Process.Signal(os.Interrupt); err != nil {
		return err
	}

	n.shuttingDown.Store(true)

	if n.shouldForceStop {
		// give it time to shutdown gracefully
		select {
		case <-n.Wait():
		case <-time.After(time.Second * shutdownGracePeriodOnForceStopInSec):
			for i := 0; i < numberOfInterruptsOnForceStop; i++ {
				_ = n.cmd.Process.Signal(os.Interrupt)
			}

			<-n.Wait()
		}
	} else {
		<-n.Wait()
	}

	return nil
}

type exitResult struct {
	Signaled bool
	Err      error
}
