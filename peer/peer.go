package peer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/bbpcr/Yomato/bitfield"
	"github.com/bbpcr/Yomato/torrent_info"
)

type PeerStatus int

const (
	DISCONNECTED PeerStatus = iota
	PENDING_HANDSHAKE
	HANDSHAKED
	CONNECTED
)

type PeerCommunication struct {
	Peer          *Peer
	BytesReceived []byte
	MessageID     int
	StatusMessage string
}

type Peer struct {
	IP           string
	Port         int
	Connection   net.Conn
	Protocol     string
	Status       PeerStatus
	TorrentInfo  *torrent_info.TorrentInfo
	LocalPeerId  string
	RemotePeerId string
	BitfieldInfo bitfield.Bitfield
}

const (
	CHOKE           = 0
	UNCHOKE         = 1
	INTERESTED      = 2
	NOT_INTERESTED  = 3
	HAVE            = 4
	BITFIELD        = 5
	REQUEST         = 6
	PIECE           = 7
	CANCEL          = 8
	PORT            = 9
	HANDSHAKE       = 10
	FULL_CONNECTION = 11
)

// GetInfo return a string consisting of peer status
func (peer *Peer) GetInfo() string {
	infoString := fmt.Sprintf("Remote IP : %s:%d", peer.IP, peer.Port)
	infoString += fmt.Sprintln()
	infoString += fmt.Sprintln("Remote peer ID : ", peer.RemotePeerId)
	infoString += fmt.Sprintln("Remote peer ID length : ", len(peer.RemotePeerId))
	infoString += fmt.Sprintln("Protocol : ", peer.Protocol)
	switch peer.Status {
	case DISCONNECTED:
		infoString += fmt.Sprintln("Status : DISCONNECTED")
	case CONNECTED:
		infoString += fmt.Sprintln("Status : CONNECTED")
	case PENDING_HANDSHAKE:
		infoString += fmt.Sprintln("Status : Pending Handshake")
	default:
		infoString += fmt.Sprintln("Status : NONE")
	}
	infoString += fmt.Sprintln("Local peer ID : ", peer.LocalPeerId)
	return infoString
}

// connect tries to get a TCP then an UDP connection for a peer
func (peer *Peer) connect() error {
	for _, protocol := range []string{"tcp", "udp"} {
		conn, err := net.DialTimeout(protocol, fmt.Sprintf("%s:%d", peer.IP, peer.Port), 1*time.Second)
		if err != nil {
			continue
		}
		peer.Connection = conn
		return nil
	}
	return errors.New("Peer not available")
}

func readExactly(connection *net.Conn, buffer []byte, length int) error {
	bytesReaded := 0

	if length > len(buffer) || length < 0 {
		return errors.New("Invalid parameters")
	}

	for bytesReaded < length {
		readed, err := (*connection).Read(buffer[bytesReaded:length])
		if err != nil {
			return err
		}
		bytesReaded += readed
	}
	return nil
}

// tryReadMessage returns (type of messasge, message, error) received by a peer
func (peer *Peer) tryReadMessage(timeout time.Duration) (int, []byte, error) {

	//First we read the first 5 bytes;
	peer.Connection.SetReadDeadline(time.Now().Add(timeout))

	buffer := make([]byte, 32*1024)
	err := readExactly(&peer.Connection, buffer, 5)

	if err != nil {
		return -1, nil, err
	}

	// Then we convert the first 4 bytes into length , 5-th byte into id , and we read the rest of the data

	length := int(binary.BigEndian.Uint32(buffer[0:4]))
	id := int(buffer[4])

	err = readExactly(&peer.Connection, buffer, length-1)

	if err != nil {
		return -1, nil, err
	}
	return id, buffer[0 : length-1], nil
}

func (peer *Peer) readExistingPieces() error {
	if (peer.Status == HANDSHAKED || peer.Status == CONNECTED) && peer.Connection != nil {
		bitfieldInfo := bitfield.New(int(peer.TorrentInfo.FileInformations.PieceCount))

		for true {
			id, data, err := peer.tryReadMessage(1 * time.Second)
			if err != nil {
				break
			}
			if id == BITFIELD {
				bitfieldInfo.Put(data, len(data))
			} else if id == HAVE {
				pieceIndex := int(binary.BigEndian.Uint32(data))
				bitfieldInfo.Set(pieceIndex, true)
			}
		}

		peer.BitfieldInfo = bitfieldInfo
		return nil
	} else {
		return errors.New("Peer not connected")
	}
	return nil

}

func (peer *Peer) sendUnchoke() error {

	if (peer.Status == HANDSHAKED || peer.Status == CONNECTED) && peer.Connection != nil {

		buf := []byte{0, 0, 0, 1, 1}
		peer.Connection.SetWriteDeadline(time.Now().Add(1 * time.Second))
		bytesWritten, err := peer.Connection.Write(buf)

		if err != nil || bytesWritten < len(buf) {
			if err != nil {
				return err
			} else {
				return errors.New(fmt.Sprintf("Insufficient bytes written"))
			}
		}
		return nil
	} else {
		return errors.New("Peer not connected")
	}
	return nil
}

func (peer *Peer) sendInterested() error {

	if (peer.Status == HANDSHAKED || peer.Status == CONNECTED) && peer.Connection != nil {

		buf := []byte{0, 0, 0, 1, 2}
		peer.Connection.SetWriteDeadline(time.Now().Add(1 * time.Second))
		bytesWritten, err := peer.Connection.Write(buf)

		if err != nil || bytesWritten < len(buf) {
			if err != nil {
				return err
			} else {
				return errors.New(fmt.Sprintf("Insufficient bytes written"))
			}
		}

		id, _, err := peer.tryReadMessage(1 * time.Second)

		if err != nil || id != UNCHOKE {
			if err != nil {
				return err
			} else {
				return errors.New("Didn't receive unchoked")
			}
		}
		return nil
	} else {
		return errors.New("Peer not connected")
	}
	return nil
}

func (peer *Peer) requestPiece(index int, begin int, length int) ([]byte, error) {

	if length+begin > int(peer.TorrentInfo.FileInformations.PieceLength) {
		length = int(peer.TorrentInfo.FileInformations.PieceLength) - begin
	}

	if index == int(peer.TorrentInfo.FileInformations.PieceCount-1) {
		if peer.TorrentInfo.FileInformations.PieceCount >= 2 {
			lastPieceLength := peer.TorrentInfo.FileInformations.TotalLength - peer.TorrentInfo.FileInformations.PieceLength*(peer.TorrentInfo.FileInformations.PieceCount-1)
			if begin+length > int(lastPieceLength) {
				length = int(lastPieceLength) - begin
			}
		}
	}
	bytesToBeWritten := make([]byte, 4*4+1)
	binary.BigEndian.PutUint32(bytesToBeWritten[0:4], 13)
	bytesToBeWritten[4] = 6
	binary.BigEndian.PutUint32(bytesToBeWritten[5:9], uint32(index))
	binary.BigEndian.PutUint32(bytesToBeWritten[9:13], uint32(begin))
	binary.BigEndian.PutUint32(bytesToBeWritten[13:], uint32(length))

	if (peer.Status == HANDSHAKED || peer.Status == CONNECTED) && peer.Connection != nil {

		peer.Connection.SetWriteDeadline(time.Now().Add(1 * time.Second))
		bytesWritten, err := peer.Connection.Write(bytesToBeWritten)

		if err != nil || bytesWritten < len(bytesToBeWritten) {

			if err != nil {
				return bytesToBeWritten[5:], err
			} else {
				return bytesToBeWritten[5:], errors.New("Insufficient bytes written")
			}
		}

		id, data, err := peer.tryReadMessage(1 * time.Second)

		if err != nil || id != PIECE {
			if err != nil {
				return bytesToBeWritten[5:], err
			} else {
				return bytesToBeWritten[5:], errors.New("Didn't receive a piece")
			}
		}
		return data, nil

	} else {
		return bytesToBeWritten[5:], errors.New("Peer not connected")
	}
	return nil , nil
}

func (peer *Peer) sendHandshake() error {

	if peer.Status == DISCONNECTED {

		err := peer.connect()
		if err == nil {
			peer.Status = PENDING_HANDSHAKE
			return peer.sendHandshake()
		} else {
			peer.Disconnect()
			return err
		}

	} else if peer.Status == PENDING_HANDSHAKE {

		protocolString := "BitTorrent protocol"
		handshake := make([]byte, 0, 48+len(protocolString))
		handshake = append(handshake, byte(19))
		handshake = append(handshake, []byte(protocolString)...)
		handshake = append(handshake, []byte{0, 0, 0, 0, 0, 0, 0, 0}...)
		handshake = append(handshake, peer.TorrentInfo.InfoHash...)
		handshake = append(handshake, []byte(peer.LocalPeerId)...)

		peer.Connection.SetDeadline(time.Now().Add(5 * time.Second))
		bytesWritten, err := peer.Connection.Write(handshake)

		if err != nil || bytesWritten < len(handshake) {

			peer.Disconnect()
			if err != nil {
				return err
			} else {
				return errors.New("Insufficient bytes written")
			}
		}

		resp := make([]byte, len(handshake))
		bytesRead, err := peer.Connection.Read(resp)

		if err != nil || bytesRead < len(resp) {

			peer.Disconnect()
			if err != nil {
				return err
			} else {
				return errors.New("Insufficient bytes read")
			}
		}

		protocol := resp[1:20]
		peer.Protocol = string(protocol)
		if string(protocol) != protocolString {

			peer.Disconnect()
			return errors.New(fmt.Sprintf("Wrong protocol %s", string(protocol)))
		}

		remotePeerId := string(resp[48:])

		peer.Status = HANDSHAKED
		peer.RemotePeerId = remotePeerId
		return nil
	} else {
		return errors.New("Invalid status")
	}
	return nil
}

// WaitForContents sends to channel comm information about downloaded content of a peer
func (peer *Peer) ReadExistingPieces(comm chan PeerCommunication) {

	err := peer.readExistingPieces()
	if err != nil {
		comm <- PeerCommunication{peer, peer.BitfieldInfo.Encode(), BITFIELD, "ERROR:" + err.Error()}
		return
	}
	comm <- PeerCommunication{peer, peer.BitfieldInfo.Encode(), BITFIELD, "OK"}
	return
}

// SendUnchoke sends to the peer to unchoke
func (peer *Peer) SendUnchoke(comm chan PeerCommunication) {

	err := peer.sendUnchoke()
	if err != nil {
		comm <- PeerCommunication{peer, nil, UNCHOKE, "ERROR:" + err.Error()}
		return
	}
	comm <- PeerCommunication{peer, nil, UNCHOKE, "OK"}
	return
}

// SendInterested sends to the peer through the main channel that it's interested
// Data transfer takes place whenever one side is interested and the other side is not choking
func (peer *Peer) SendInterested(comm chan PeerCommunication) {

	err := peer.sendInterested()
	if err != nil {
		comm <- PeerCommunication{peer, nil, INTERESTED, "ERROR:" + err.Error()}
		return
	}
	comm <- PeerCommunication{peer, nil, INTERESTED, "OK"}
	return
}

// RequestPiece makes a request to tracker to obtain 'length' bytes from 'begin'
// the data is sent to channel 'comm'.
func (peer *Peer) RequestPiece(comm chan PeerCommunication, index int, begin int, length int) {

	data, err := peer.requestPiece(index, begin, length)
	if err != nil {
		comm <- PeerCommunication{peer, data, REQUEST, "ERROR:" + err.Error()}
		return
	}
	comm <- PeerCommunication{peer, data, REQUEST, "OK"}
	return
}

// Disconnect closes the connection of a peer.
func (peer *Peer) Disconnect() {

	peer.Status = DISCONNECTED
	if peer.Connection != nil {
		peer.Connection.Close()
	}
	peer.Connection = nil
	return
}

// Handshake attempts to set up the first message transmitted by the peer, sending the response through comm
func (peer *Peer) Handshake(comm chan PeerCommunication) {
	err := peer.sendHandshake()
	if err != nil {
		comm <- PeerCommunication{peer, nil, HANDSHAKE, "ERROR:" + err.Error()}
		return
	}
	comm <- PeerCommunication{peer, nil, HANDSHAKE, "OK"}
	return
}

func (peer *Peer) EstablishFullConnection(comm chan PeerCommunication) {

	if peer.Status == CONNECTED || peer.Status == PENDING_HANDSHAKE || peer.Status == HANDSHAKED {
		return
	}

	err := peer.sendHandshake()
	if err != nil {
		comm <- PeerCommunication{peer, nil, FULL_CONNECTION, "ERROR:" + err.Error()}
		return
	}

	err = peer.readExistingPieces()
	if err != nil {
		peer.Disconnect()
		comm <- PeerCommunication{peer, nil, FULL_CONNECTION, "ERROR:" + err.Error()}
		return
	}

	err = peer.sendUnchoke()
	if err != nil {
		peer.Disconnect()
		comm <- PeerCommunication{peer, nil, FULL_CONNECTION, "ERROR:" + err.Error()}
		return
	}

	err = peer.sendInterested()
	if err != nil {
		peer.Disconnect()
		comm <- PeerCommunication{peer, nil, FULL_CONNECTION, "ERROR:" + err.Error()}
		return
	}

	peer.Status = CONNECTED
	comm <- PeerCommunication{peer, nil, FULL_CONNECTION, "OK"}
	return
}

// New returns a peer with given description
func New(torrentInfo *torrent_info.TorrentInfo, peerId string, ip string, port int) Peer {
	return Peer{
		IP:          ip,
		Port:        port,
		Status:      DISCONNECTED,
		TorrentInfo: torrentInfo,
		LocalPeerId: peerId,
	}
}
