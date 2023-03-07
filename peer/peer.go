package peer

import (
	"math"
	"math/rand"
	"net"
	"strconv"
	"time"

	"github.com/golang/glog"
)

type KeepaliveStats struct {
	TotalMmisses   int
	LastChangeUp   int64
	LastChangeDown int64
}

type SessionState struct {
	RemotePeer string
	PeerState  PeerState
}

// Manager defines methods to control and query a keepalive process
type Peer interface {
	Start()
	Stop()
	GetSessionStateChangeCh() chan *SessionState
	GetRemoteAddr() string
	Alive() bool
	// GetKeepaliveCh() chan *message.Keepalive
	// GetStats() *KeepaliveStats
}

type peer struct {
	msg            *Keepalive
	local          *net.UDPConn
	localAddr      *net.UDPAddr
	remote         *net.UDPConn
	remoteAddr     *net.UDPAddr
	stateCh        chan *SessionState
	alive          bool
	isAlive        chan bool
	misses         int
	totalMisses    int
	lastChangeUp   int64
	lastChangeDown int64
	stop           chan struct{}
}

func (p *peer) Start() {
	glog.Infof("Starting keepalive manager for remote peer %s", p.remoteAddr.String())
	go p.manager()
}

func (p *peer) Alive() bool {
	p.isAlive <- true
	return <-p.isAlive
}

func (p *peer) Stop() {
	glog.Infof("Stopping keepalive processing for remote peer %s", p.remoteAddr.String())
	close(p.stop)
	p.local.Close()
	p.remote.Close()
}

func (p *peer) GetSessionStateChangeCh() chan *SessionState {
	return p.stateCh
}

func (p *peer) GetRemoteAddr() string {
	return p.remoteAddr.String()
}

func (p *peer) GetStats() *KeepaliveStats {
	return &KeepaliveStats{
		TotalMmisses:   p.totalMisses,
		LastChangeUp:   p.lastChangeUp,
		LastChangeDown: p.lastChangeDown,
	}
}

func (p *peer) rxKeepalive(alive chan *Keepalive, errCh chan error, stop chan struct{}) {
	b := make([]byte, KeepaliveMessageLen)
	// Adding 25% to the read deadline timer to prevent false positives on a busy system
	interval := time.Duration(p.msg.Interval+p.msg.Interval/100*25) * time.Millisecond
	for {
		select {
		case <-stop:
			glog.V(5).Infof("stopping keepalive Rx")
			close(stop)
			return
		default:
		}
		if err := p.local.SetReadDeadline(time.Now().Add(interval)); err != nil {
			//			go func() {
			errCh <- err
			//			}()
			continue
		}
		_, err := p.local.Read(b)
		if err == nil {
			msg := &Keepalive{}
			if err := msg.UnmarshalBinary(b); err != nil {
				// Break from the processing to send the error to the manager
				break
			}
			//			go func() {
			alive <- msg
			//			}()
			continue
		}
		//		go func() {
		errCh <- err
		//		}()
	}
}

func (p *peer) txKeepalive(errCh chan error, stop chan struct{}) {
	txTimer := time.NewTicker(time.Duration(p.msg.Interval) * time.Millisecond)
	b := p.msg.MarshalBinary()
	for {
		select {
		case <-txTimer.C:
			if err := p.remote.SetWriteDeadline(time.Now().Add(time.Duration(p.msg.Interval) * time.Millisecond)); err != nil {
				//				go func() {
				errCh <- err
				//				}()
			}
			if _, err := p.remote.Write(b); err != nil {
				//				go func() {
				errCh <- err
				//				}()
			}
		case <-stop:
			glog.V(5).Infof("stopping keepalive Tx")
			txTimer.Stop()
			close(stop)
			return
		}
	}

}

func (p *peer) manager() {
	txErr := make(chan error)
	stopTx := make(chan struct{})
	stopRx := make(chan struct{})
	rxErr := make(chan error)
	keepalive := make(chan *Keepalive)
	// Starting reciever
	go p.rxKeepalive(keepalive, rxErr, stopRx)
	// Starting transmitter
	go p.txKeepalive(txErr, stopTx)
	for {
		select {
		case <-p.stop:
			stopTx <- struct{}{}
			<-stopTx
			stopRx <- struct{}{}
			<-stopRx
			glog.Infof("keepalive process manager received stop signal")
			return
		case err := <-txErr:
			if p.alive {
				glog.Errorf("keepalive session with %s lost, due to keepalive tx error: %+v", p.remoteAddr.String(), err)
				// Informing the higher level process about lost keepalive session
				p.lastChangeDown = time.Now().Unix()
				//				go func() {
				p.stateCh <- &SessionState{
					RemotePeer: p.remoteAddr.String(),
					PeerState:  PeerDown,
				}
				//				}()
			}
			p.alive = false
		case err := <-rxErr:
			p.totalMisses++
			if !p.alive {
				continue
			}
			glog.Errorf("keepalive rx reported error: %+v", err)
			// Keepalive session is still alive, checking if exceeding the dead interval
			p.misses++
			glog.Infof("missed keepalive from %s, number of missed keepalives: %d", p.remoteAddr.String(), p.misses)
			if p.misses > int(p.msg.DeadInterval)/int(p.msg.Interval) {
				p.lastChangeDown = time.Now().Unix()
				// Informing the higher level process about lost keepalive session
				//				go func() {
				p.stateCh <- &SessionState{
					RemotePeer: p.remoteAddr.String(),
					PeerState:  PeerDown,
				}
				//				}()
				p.alive = false
			}
		case <-keepalive:
			p.misses = 0
			if !p.alive {
				p.alive = true
				p.lastChangeUp = time.Now().Unix()
				// Informing that the connection with peer is Up
				//				go func() {
				p.stateCh <- &SessionState{
					RemotePeer: p.remoteAddr.String(),
					PeerState:  PeerUp,
				}
				//				}()
			}
		case <-p.isAlive:
			p.isAlive <- p.alive
		}
	}
}

func NewPeer(la string, port int, ra string, state chan *SessionState) (Peer, error) {
	p := &peer{
		msg: &Keepalive{
			Priority:     0,
			Interval:     1000,
			DeadInterval: 3000,
		},
		stateCh: state,
		alive:   false,
		isAlive: make(chan bool),
		stop:    make(chan struct{}),
	}
	l, err := net.ResolveUDPAddr("udp", la+":"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	p.localAddr = l
	p.local, err = net.ListenUDP("udp", l)
	if err != nil {
		return nil, err
	}
	r, err := net.ResolveUDPAddr("udp", ra+":"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	p.remoteAddr = r
	// Getting a random number for UDP TX
	rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
	lport := math.MaxUint16 - rand.Intn(10240)
	lu, err := net.ResolveUDPAddr("udp", la+":"+strconv.Itoa(lport))
	if err != nil {
		return nil, err
	}
	p.remote, err = net.DialUDP("udp", lu, r)
	if err != nil {
		return nil, err
	}

	return p, nil
}
