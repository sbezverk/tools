package tools

import (
	"os"
	"os/signal"
)

var (
	onlyOneSignalHandler = make(chan struct{})
	shutdownSignals      = []os.Signal{os.Interrupt}
)

//  SetupSignalHandler creates and returns a channel. The channel will be closed up on receiving
// the interrupt signal (Ctrl-C), if a second interrupt signal is received, the calling program is forced to terminate.
func SetupSignalHandler() (stopCh <-chan struct{}) {
	close(onlyOneSignalHandler) // panics when called twice

	stop := make(chan struct{})
	c := make(chan os.Signal, 2)
	signal.Notify(c, shutdownSignals...)
	go func() {
		<-c
		close(stop)
		<-c
		os.Exit(1)
	}()

	return stop
}
