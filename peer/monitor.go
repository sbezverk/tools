package peer

import (
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"

	"github.com/golang/glog"
)

type PeerState uint8

func (ps PeerState) String() string {
	if ps == PeerUp {
		return "UP"
	}
	return "DOWN"
}

const (
	PeerUp PeerState = iota + 1
	PeerDown
)

type MonitorMessage struct {
	RemotePeer string
	PeerState  PeerState
}
type Monitor interface {
	// GetPeerState(string) PeerState
	GetMonitorCh() chan *MonitorMessage
	Stop()
}

var _ Monitor = &monitor{}

type monitor struct {
	stop            chan struct{}
	monitorCh       chan *MonitorMessage
	monitorChActive bool
}

func (m *monitor) Stop() {
	glog.Infof("Closing monitor...")
	m.stop <- struct{}{}
	<-m.stop
	glog.Infof("monitor closed ..")
}

func (m *monitor) GetMonitorCh() chan *MonitorMessage {
	m.monitorChActive = true
	return m.monitorCh
}

func (m *monitor) manager(la string, port int, rPeers ...string) {
	peers := make(map[string]Peer)
	peersStateCh := make(chan *SessionState)
	for _, p := range rPeers {
		rp, err := NewPeer(la, port, p, peersStateCh)
		if err != nil {
			glog.Errorf("failed creating remote peer %s with error: %+v", p, err)
			continue
		}
		peers[rp.GetRemoteAddr()] = rp
		rp.Start()
	}
	for {
		select {
		case <-m.stop:
			for _, p := range peers {
				glog.Infof("Closing keepalive session with %s", p.GetRemoteAddr())
				p.Stop()
			}
			close(m.stop)
			return
		case msg := <-peersStateCh:
			glog.Infof("Peer: %s state changed to %s", msg.RemotePeer, msg.PeerState)
		}
	}
}

// SetupMonitorForRemotePeer sets up a keep alive mechanism between a local and remote peers,
// returned Monitor interface allows get a state of a particular peer by specifying its id, or get
// a notification channel for changes of a remote peer states.
func SetupMonitorForRemotePeer(la string, port int, rPeers ...string) (Monitor, error) {
	// If number of remote peers is 0, return an error as nothing to do
	if len(rPeers) == 0 {
		return nil, fmt.Errorf("no remote peers specified")
	}
	// Validate the port to use for listening of Keepalive messages
	if port >= math.MaxUint16 {
		return nil, fmt.Errorf("invalid value %d for the port", port)
	}
	// Validate the list of remote peers IPs to be a valid IP addresses and
	// to belong to the same address family, ipv4 or ipv6. Mixed remote peers are not supported
	_, _, isIPv4, err := validateIPAddress(la)
	if err != nil {
		return nil, err
	}
	for _, rp := range rPeers {
		_, _, ipv4, err := validateIPAddress(rp)
		if err != nil {
			return nil, err
		}
		if !isIPv4 && ipv4 {
			return nil, fmt.Errorf("remote peer %s and local peer %s belong to different address families, it is not supported", la, rp)
		}
	}
	m := &monitor{
		stop:            make(chan struct{}),
		monitorCh:       make(chan *MonitorMessage),
		monitorChActive: false,
	}
	go m.manager(la, port, rPeers...)

	return m, nil
}

func validateIPAddress(route string) (string, int, bool, error) {
	// Validating route
	p := strings.Split(route, "/")
	var prfx string
	var prfxLength string
	switch len(p) {
	case 1:
		prfx = p[0]
	case 2:
		prfx = p[0]
		prfxLength = p[1]
	default:
		return "", 0, false, fmt.Errorf("invalid format")
	}
	addr := net.ParseIP(prfx)
	if addr.To16() == nil && addr.To4() == nil {
		return "", 0, false, fmt.Errorf("invalid format of a prefix %s", prfx)
	}
	isIPv4 := false
	if addr.To4() != nil {
		isIPv4 = true
	}
	if prfxLength == "" {
		prfxLength = "0"
	}
	l, err := strconv.Atoi(prfxLength)
	if err != nil {
		return "", 0, false, fmt.Errorf("invalid prefix length")
	}
	if (l > 128 && !isIPv4) || (l > 32 && isIPv4) {
		return "", 0, false, fmt.Errorf("invalid prefix length %d value for prefix %s", l, prfx)
	}

	return prfx, l, isIPv4, nil
}
