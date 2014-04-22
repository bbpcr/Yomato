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
		if protocol == "tcp" {
			conn.(*net.TCPConn).SetKeepAlive(true)
			conn.(*net.TCPConn).SetNoDelay(false)
			conn.(*net.TCPConn).SetReadBuffer(16 * 1024)
		} else {

		}
		peer.Connection = conn
		return nil
	}
	return errors.New("Peer not available")
}

func readExactly(connection net.Conn, buffer []byte, length int) error {
	bytesReaded := 0

	if length > len(buffer) || length < 0 {
		return errors.New("Invalid parameters")
	}

	var readed int
	var err error
	for bytesReaded < length {
		readed, err = connection.Read(buffer[bytesReaded:length])
		if err != nil {
			return err
		}
		bytesReaded += readed
	}
	return nil
}

// tryReadMessage returns (type of messasge, message, error) received by a peer
func (peer *Peer) tryReadMessage(timeout time.Duration, maxBufferSize int) (int, []byte, error) {

	// First we read the first 5 bytes;
	peer.Connection.SetReadDeadline(time.Now().Add(timeout))

	buffer := make([]byte, maxBufferSize)
	err := readExactly(peer.Connection, buffer, 5)

	if err != nil {
		return -1, nil, err
	}

	// Then we convert the first 4 bytes into length , 5-th byte into id , and we read the rest of the data

	length := int(binary.BigEndian.Uint32(buffer[0:4]))
	id := int(buffer[4])

	err = readExactly(peer.Connection, buffer, length-1)

	if err != nil {
		return -1, nil, err
	}
	return id, buffer[0 : length-1], nil
}

// Reads all the bitfield and have messages
// Peers should immediately send his bitfield,
// so the client knows what pieces the peer has.
func (peer *Peer) readExistingPieces() error {
	if (peer.Status == HANDSHAKED || peer.Status == CONNECTED) && peer.Connection != nil {

		// Create the bitfield with the length equal to the number of pieces
		bitfieldInfo := bitfield.New(int(peer.TorrentInfo.FileInformations.PieceCount))

		for true {

			// Read exactly one message
			id, data, err := peer.tryReadMessage(1*time.Second, int(bitfieldInfo.Length)+1)
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

// Sends and unchoke message to the peer
// The message is exactly : [0, 0, 0, 1, 1] (first four bytes length = 1 , last byte the id of the message = 1).
// Peers wont respond to block requests if they are choked and uninterested.
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

// Sends an interested message to the peer.
// The message is exactly : [0, 0, 0, 1, 2] (first four bytes length = 1 , last byte the id of the message = 2).
// Peers wont respond to block requests if they are choked and uninterested.
func (peer *Peer) sendInterested() error {

	if (peer.Status == HANDSHAKED || peer.Status == CONNECTED) && peer.Connection != nil {

		buf := []byte{0, 0, 0, 1, INTERESTED}
		peer.Connection.SetWriteDeadline(time.Now().Add(1 * time.Second))
		bytesWritten, err := peer.Connection.Write(buf)
		// Writes the byte array to the peer

		if err != nil || bytesWritten < len(buf) {
			if err != nil {
				return err
			} else {
				return errors.New(fmt.Sprintf("Insufficient bytes written"))
			}
		}

		// Most peers response immediately with unchoke after sending an unchoke and interested,
		// Max buffer size should be 5 because unchoke size is 5.
		id, _, err := peer.tryReadMessage(1*time.Second, 5)

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

// Request multiple blocks on the peers
// Parameters are like this : an array multiple of three,
// [index1, begin1, length1, index2, begin2, length2,...] and so on..
// This writes the byte form of the input on the connection
func (peer *Peer) WriteRequest(params []int) error {
	requestBytes := []byte{}

	// Start creating the bytes for the request

	for request := 0; request < len(params); request += 3 {

		signatureBytes := make([]byte, 5)
		binary.BigEndian.PutUint32(signatureBytes[0:4], 13)
		signatureBytes[4] = REQUEST
		// The first 4 bytes are the length of one request which is 13 = sizeof(ID) + sizeof(index) + sizeof(begin) + sizeof(length)
		requestBytes = append(requestBytes, signatureBytes...)
		requestBytes = append(requestBytes, convertIntsToByteArray(params[request], params[request+1], params[request+2])...)
		// We create one big byte array containing all the requests
	}
	if (peer.Status == HANDSHAKED || peer.Status == CONNECTED) && peer.Connection != nil {
		peer.Connection.SetWriteDeadline(time.Now().Add(1 * time.Second))
		_, err := peer.Connection.Write(requestBytes)
		return err
	} else {
		return errors.New("Peer not connected")
	}
	return nil
}

// This converts an array of ints into a byte array
// ex : input [567 , 8978] -> output [0 0 2 55 0 0 35 18]
func convertIntsToByteArray(params ...int) []byte {
	buffer := make([]byte, 4*len(params))
	for index := 0; index < 4*len(params); index += 4 {
		binary.BigEndian.PutUint32(buffer[index:index+4], uint32(params[index/4]))
	}
	return buffer
}

// Reads the piece messages from the connection
// and returns the bytes like this : [<index1><begin1><length1><block1><index2><begin2><length2><block2>]
// This function always returs the input parameters in the end with length 0. This helps for unmarking the downloading blocks.
func (peer *Peer) readBlocks(params []int) ([]byte, error) {

	queue_length := len(params) / 3

	if (peer.Status == HANDSHAKED || peer.Status == CONNECTED) && peer.Connection != nil {

		receivedBytes := []byte{}

		for request := 0; request < queue_length; request++ {

			// Read on message from the connection , using a 17kb buffer. (One message cannot be higher than 17kb)
			id, data, err := peer.tryReadMessage(1*time.Second, 17*1024)
			// If it encounters an error , then we stop reading.
			if err != nil {
				break
			}
			if id == PIECE {
				receivedBytes = append(receivedBytes, data[0:8]...)
				receivedBytes = append(receivedBytes, convertIntsToByteArray(len(data[8:]))...)
				receivedBytes = append(receivedBytes, data[8:]...)
			}
			// Append the bytes
		}

		// Always append the input parameters to the result.
		for request := 0; request < len(params); request += 3 {
			receivedBytes = append(receivedBytes, convertIntsToByteArray(params[request], params[request+1], 0)...)
		}

		// If the length of bytes received is 12 (we didn't read anything)
		// this returns also an error, so we know to handle the peer differently
		if len(receivedBytes) == 12*queue_length {
			return receivedBytes, errors.New("Nothing readed")
		} else {
			return receivedBytes, nil
		}

	} else {
		receivedBytes := []byte{}
		for request := 0; request < 3*queue_length; request += 3 {
			receivedBytes = append(receivedBytes, convertIntsToByteArray(params[request], params[request+1], 0)...)
		}
		return receivedBytes, errors.New("Peer not connected")
	}
	return nil, nil
}

// Sends a handshake to the peer.
// This is mandatory to call this first , when initializing a connection with the peer,
// because it won't response to any message until a handshake has been done.
func (peer *Peer) sendHandshake() error {

	if peer.Status == DISCONNECTED {
		//If the peer is disconnected,
		//it connects to the ip and port that we have.
		err := peer.connect()
		if err == nil {
			peer.Status = PENDING_HANDSHAKE
			return peer.sendHandshake()
		} else {
			peer.Disconnect()
			return err
		}

	} else if peer.Status == PENDING_HANDSHAKE {

		// At this point , it is connected to the peer.
		// It will send a byte array like this :
		// <protocolStringLength><protocolString><reservedBytes><infoHash><peerID>
		// with exactly (48 + protocolStringLength) bytes

		protocolString := "BitTorrent protocol"
		handshake := make([]byte, 0, 48+len(protocolString))
		handshake = append(handshake, byte(19))
		handshake = append(handshake, []byte(protocolString)...)
		handshake = append(handshake, []byte{0, 0, 0, 0, 0, 0, 0, 0}...)
		handshake = append(handshake, peer.TorrentInfo.InfoHash...)
		handshake = append(handshake, []byte(peer.LocalPeerId)...)

		peer.Connection.SetDeadline(time.Now().Add(5 * time.Second))
		// Set a higher timeout, because some peers respond slower at handshake.
		bytesWritten, err := peer.Connection.Write(handshake)

		if err != nil || bytesWritten < len(handshake) {

			peer.Disconnect()
			if err != nil {
				return err
			} else {
				return errors.New("Insufficient bytes written")
			}
		}

		// At this point , the peer should send us exactly the same size that we requested.
		resp := make([]byte, len(handshake))
		err = readExactly(peer.Connection, resp, len(resp))

		if err != nil {
			peer.Disconnect()
			return err
		}

		// Some peers send wrong protocol , so we disconnect it.
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

func (peer *Peer) ReadExistingPieces(comm chan PeerCommunication) {

	err := peer.readExistingPieces()
	if err != nil {
		comm <- PeerCommunication{peer, peer.BitfieldInfo.Encode(), BITFIELD, "ERROR:" + err.Error()}
		return
	}
	comm <- PeerCommunication{peer, peer.BitfieldInfo.Encode(), BITFIELD, "OK"}
	return
}

func (peer *Peer) SendUnchoke(comm chan PeerCommunication) {

	err := peer.sendUnchoke()
	if err != nil {
		comm <- PeerCommunication{peer, nil, UNCHOKE, "ERROR:" + err.Error()}
		return
	}
	comm <- PeerCommunication{peer, nil, UNCHOKE, "OK"}
	return
}

func (peer *Peer) SendInterested(comm chan PeerCommunication) {

	err := peer.sendInterested()
	if err != nil {
		comm <- PeerCommunication{peer, nil, INTERESTED, "ERROR:" + err.Error()}
		return
	}
	comm <- PeerCommunication{peer, nil, INTERESTED, "OK"}
	return
}

func (peer *Peer) ReadBlocks(comm chan PeerCommunication, params []int) {

	data, err := peer.readBlocks(params)
	if err != nil {
		comm <- PeerCommunication{peer, data, REQUEST, "ERROR:" + err.Error()}
		return
	}
	comm <- PeerCommunication{peer, data, REQUEST, "OK"}
	return
}

// Disconnect closes the connection of a peer,
// and sets the status to DISCONNECTED.
func (peer *Peer) Disconnect() {

	peer.Status = DISCONNECTED
	if peer.Connection != nil {
		peer.Connection.Close()
	}
	peer.Connection = nil
	return
}

func (peer *Peer) Handshake(comm chan PeerCommunication) {
	err := peer.sendHandshake()
	if err != nil {
		comm <- PeerCommunication{peer, nil, HANDSHAKE, "ERROR:" + err.Error()}
		return
	}
	comm <- PeerCommunication{peer, nil, HANDSHAKE, "OK"}
	return
}

// Establishes full connection with the peer.
// Full connection means : handshake , reading the bitfield and
// sending unchoke and interested to the peer.
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
