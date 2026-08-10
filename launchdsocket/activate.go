package launchdsocket

import (
	"errors"
	"net"

	launchd "github.com/tprasadtp/go-launchd"
)

var ErrNoActivatedSocket = errors.New("launchdsocket: no activated socket available")

func ActivateNamedSocket(name string) ([]net.Listener, error) {
	if name == "" {
		return nil, errors.New("launchdsocket: socket name is required")
	}
	listeners, err := launchd.Listeners(name)
	if err != nil {
		return nil, err
	}
	if len(listeners) == 0 {
		return nil, ErrNoActivatedSocket
	}
	return listeners, nil
}
