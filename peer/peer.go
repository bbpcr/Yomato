package peer

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/bbpcr/Yomato/torrent_info"
)

type PeerStatus int

const (
	Disconnected PeerStatus = iota
	PendingHandshake
	Connected
)

type PeerCommunication struct {
	Peer    *Peer
	Message string
}

type Peer struct {
	IP           string
	Port         int
	Conn         *net.Conn
	Protocol     string
	Status       PeerStatus
	TorrentInfo  *torrent_info.TorrentInfo
	LocalPeerId  string
	RemotePeerId string
}

func (p *Peer) connect(callback func(error)) {
	go (func() {
		for _, protocol := range []string{"tcp", "udp"} {
			conn, err := net.DialTimeout(protocol, fmt.Sprintf("%s:%d", p.IP, p.Port), 1*time.Second)
			if err != nil {
				continue
			}
			p.Conn = &conn
			callback(nil)
			return
		}
		callback(errors.New("Peer not available"))
	})()
}

func (p *Peer) Handshake(comm chan PeerCommunication) {
	if p.Status == Disconnected || p.Conn == nil {
		p.connect(func(err error) {
			if err == nil {
				p.Status = PendingHandshake
				p.Handshake(comm)
			} else {
				comm <- PeerCommunication{p, fmt.Sprintf("Error: %s", err)}
			}
		})
		return
	} else if p.Status != PendingHandshake {
		comm <- PeerCommunication{p, fmt.Sprintf("Error: Invalid status: %d", p.Status)}
		return
	}

	go (func() {
		protocolString := "BitTorrent protocol"
		handshake := make([]byte, 0, 68)
		handshake = append(handshake, byte(19))
		handshake = append(handshake, []byte(protocolString)...)
		handshake = append(handshake, []byte{0, 0, 0, 0, 0, 0, 0, 0}...)
		handshake = append(handshake, p.TorrentInfo.InfoHash...)
		handshake = append(handshake, []byte(p.LocalPeerId)...)

		(*p.Conn).Write(handshake)

		resp := make([]byte, 68)
		_, err := bufio.NewReader(*p.Conn).Read(resp)
		if err != nil {
			comm <- PeerCommunication{p, fmt.Sprintf("Error: %s", err)}
			return
		}

		protocol := resp[1:20]
		if string(protocol) != protocolString {
			comm <- PeerCommunication{p, fmt.Sprintf("Wrong protocol: %s", string(protocol))}
			return
		}
		remotePeerId := string(resp[48:])

		p.Status = Connected
		p.RemotePeerId = remotePeerId

		comm <- PeerCommunication{p, "OK"}
	})()
}

func New(torrentInfo *torrent_info.TorrentInfo, peerId string, ip string, port int) Peer {
	return Peer{
		IP:          ip,
		Port:        port,
		Status:      Disconnected,
		TorrentInfo: torrentInfo,
		LocalPeerId: peerId,
	}
}
