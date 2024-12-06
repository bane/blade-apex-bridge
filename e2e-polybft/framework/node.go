package framework

import (
	"context"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"time"
)

const (
	numberOfInterruptsOnForceStop       = 5
	shutdownGracePeriodOnForceStopInSec = 10

	ExecutionStatusRunning  = 0
	ExecutionStatusFinished = 1
	ExecutionStatusSignaled = 2
)

type Node struct {
	cmd             *exec.Cmd
	doneCh          chan struct{}
	exitResult      *exitResult
	shouldForceStop bool
	executionStatus int32
}

func NewNode(binary string, args []string, stdout io.Writer) (*Node, error) {
	return newNode(exec.Command(binary, args...), stdout)
}

func NewNodeWithContext(ctx context.Context, binary string, args []string, stdout io.Writer) (*Node, error) {
	node, err := newNode(exec.Command(binary, args...), stdout)
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()

		_ = node.Stop()
	}()

	return node, nil
}

func newNode(cmd *exec.Cmd, stdout io.Writer) (*Node, error) {
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

	notSignaled := atomic.CompareAndSwapInt32(&n.executionStatus, ExecutionStatusRunning, ExecutionStatusFinished)
	n.exitResult = &exitResult{
		Signaled: !notSignaled,
		Err:      err,
	}
	close(n.doneCh)
	n.cmd = nil
}

func (n *Node) IsShuttingDown() bool {
	return atomic.LoadInt32(&n.executionStatus) != ExecutionStatusRunning
}

func (n *Node) Stop() error {
	if !atomic.CompareAndSwapInt32(&n.executionStatus, ExecutionStatusRunning, ExecutionStatusSignaled) {
		// the server is already stopped
		return nil
	}

	if err := n.cmd.Process.Signal(os.Interrupt); err != nil {
		return err
	}

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
