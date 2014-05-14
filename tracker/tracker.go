//Package tracker implements basic functionalities offered by a Torrent Tracker
package tracker

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/bbpcr/Yomato/torrent_info"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/bbpcr/Yomato/bencode"
	"github.com/bbpcr/Yomato/peer"
)

type Tracker struct {
	AnnounceUrl string
	TorrentInfo *torrent_info.TorrentInfo
	PeerId      string
	LocalServer *http.Server
	Port        int
}

type TrackerResponse struct {
	AnnounceUrl    string
	FailureReason  string
	WarningMessage string
	Interval       int64
	MinInterval    int64
	TrackerID      string
	Complete       int64
	Incomplete     int64
	Peers          []peer.Peer
}

const (
	NONE = iota
	DOWNLOAD_COMPLETED
	DOWNLOAD_STARTED
	DOWNLOAD_STOPPED
)

// readPeersFromAnnouncer returns peers from announceUrl
func readPeersFromAnnouncer(announceUrl string, peerID string, infoHash string, port int, uploaded int64, downloaded int64, left int64, event int) (bencode.Bencoder, error) {

	qs := url.Values{}
	qs.Add("peer_id", peerID)
	qs.Add("info_hash", infoHash)
	qs.Add("port", fmt.Sprintf("%d", port))
	qs.Add("uploaded", fmt.Sprintf("%d", uploaded))
	qs.Add("downloaded", fmt.Sprintf("%d", downloaded))
	qs.Add("left", fmt.Sprintf("%d", left))
	qs.Add("compact", "1")
	switch event {
	case DOWNLOAD_STARTED:
		qs.Add("event", "started")
	case DOWNLOAD_STOPPED:
		qs.Add("event", "stopped")
	case DOWNLOAD_COMPLETED:
		qs.Add("event", "completed")
	case NONE:
	default:
		return bencode.Dictionary{}, errors.New("Invalid event")
	}
	qs.Add("numwant", "10000")
	qs.Add("key", "32896")

	requestUrl, err := url.Parse(announceUrl + "?" + qs.Encode())

	if err != nil {
		return nil, errors.New("Malformed URL")
	}

	if requestUrl.Scheme == "http" {

		//To have a timeout at request
		//you need to set up your own Client with your own Transport
		//which uses a custom Dial function which wraps around DialTimeout.

		transport := http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return net.DialTimeout(network, addr, 1*time.Second)
			},
		}

		client := http.Client{
			Transport: &transport,
		}

		response, err := client.Get(requestUrl.String())

		if err != nil {
			return bencode.Dictionary{}, err
		}
		defer response.Body.Close()

		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return bencode.Dictionary{}, err
		}

		if response.StatusCode == 200 {
			data, _, err := bencode.Parse(body)
			if err != nil {
				return bencode.Dictionary{}, err
			}

			newData, err := GetPeers(data)

			if err != nil {
				return bencode.Dictionary{}, err
			}

			return newData, nil
		}
		return bencode.Dictionary{}, errors.New(fmt.Sprintf("Expected 200 OK from tracker; got %s", response.Status))
	} else if requestUrl.Scheme == "udp" {

		adress, err := net.ResolveUDPAddr("udp", requestUrl.Host)
		if err != nil {
			return bencode.Dictionary{}, err
		}
		udpConnection, err := net.DialUDP("udp", nil, adress)
		if err != nil {
			return bencode.Dictionary{}, err
		}
		defer udpConnection.Close()
		// http://code.google.com/p/udpt/wiki/UDPTrackerProtocol

		// First step. We make ourself known.
		connectionRequestBytes := make([]byte, 8+4+4)
		binary.BigEndian.PutUint64(connectionRequestBytes[0:8], 0x41727101980) // first 8 bytes are the connection id
		binary.BigEndian.PutUint32(connectionRequestBytes[8:12], 0)            // next, 4 bytes are the action(0 for connection request)
		binary.BigEndian.PutUint32(connectionRequestBytes[12:16], 1000)        // last 4 bytes are the transction id (random number)

		udpConnection.SetWriteDeadline(time.Now().Add(1 * time.Second))
		_, err = udpConnection.Write(connectionRequestBytes)
		if err != nil {
			return bencode.Dictionary{}, err
		}

		// Step two , we read from the server. We should receive exactly the same format.
		// First 4 bytes action , next 4 bytes transaction id , last 8 bytes connection id which we will use it next

		udpConnection.SetReadDeadline(time.Now().Add(1 * time.Second))
		buffer := make([]byte, 16)
		bytesReceived, err := udpConnection.Read(buffer)
		if err != nil || bytesReceived < 16 {
			return bencode.Dictionary{}, err
		}

		receivedAction := binary.BigEndian.Uint32(buffer[0:4])
		receivedTransactionID := binary.BigEndian.Uint32(buffer[4:8])
		receivedConnectionID := binary.BigEndian.Uint64(buffer[8:16])
		if receivedAction != 0 || receivedTransactionID != 1000 {
			return bencode.Dictionary{}, errors.New("Unable to make a connection with the udp server")
		}

		//At this point we have connected succesfully to the udp server.Now we can begin to request peers

		//Step three , we send the request for peers.

		/*
			Offset      Size				Name				Value
				0	 8 (64 bit integer)	 connection  id	 connection id from server
				8	 4 (32-bit integer)	 action	 1;  for announce request
				12	 4 (32-bit integer)	 transaction id	 client can make up another transaction id...
				16	 20	                 info_hash	 the info_hash of the torrent that is being announced
				36	 20	                 peer id	 the peer ID of the client announcing itself
				56	 8 (64 bit integer)	 downloaded	 bytes downloaded by client this session
				64	 8 (64 bit integer)	 left	     bytes left to complete the download
				72	 8 (64 bit integer)	 uploaded	 bytes uploaded this session
				80	 4 (32 bit integer)	 event	     0=None; 1=Download completed; 2=Download started; 3=Download stopped.
				84	 4 (32 bit integer)	 IPv4	     IP address, default set to 0 (use source address)
				88	 4 (32 bit integer)	 key	     ?
				92	 4 (32 bit integer)	 num want	 -1 by default. number of clients to return
				96	 2 (16 bit integer)	 port	     the client's TCP port
		*/

		announceRequest := make([]byte, 98)
		binary.BigEndian.PutUint64(announceRequest[0:8], receivedConnectionID) // first 8 bytes are the connection id from server
		binary.BigEndian.PutUint32(announceRequest[8:12], 1)                   // next, 4 bytes are the action number (in this case is 1)
		binary.BigEndian.PutUint32(announceRequest[12:16], 1000)               // next, 4 bytes are the transaction id (random number)
		copy(announceRequest[16:36], []byte(infoHash))                         // next, 20 bytes are the info hash
		copy(announceRequest[36:56], []byte(peerID))                           // next, 20 bytes are the peerID
		binary.BigEndian.PutUint64(announceRequest[56:64], uint64(downloaded)) //next, 8 bytes are the downloaded size
		binary.BigEndian.PutUint64(announceRequest[64:72], uint64(left))       // next, 8 bytes are the left size
		binary.BigEndian.PutUint64(announceRequest[72:80], uint64(uploaded))   // next, 8 bytes are the uploaded size
		binary.BigEndian.PutUint32(announceRequest[80:84], uint32(event))      // next, 4 bytes are the action ( in this case is Downloaded Started = 2)
		IPV4 := []byte{127, 0, 0, 1}
		copy(announceRequest[84:88], IPV4)                        // next, 4 bytes is the true ip of the machine. Doesnt matter what you put here.
		binary.BigEndian.PutUint32(announceRequest[88:92], 32896) // next, 4 bytes is the key. I have written 32896 : [0 0 128 128]
		binary.BigEndian.PutUint32(announceRequest[92:96], 10000) // next, 4 bytes is the number of max peers to receive.
		binary.BigEndian.PutUint16(announceRequest[96:98], 80)    // last 2 bytes is the port number

		bytesWritten, err := udpConnection.Write(announceRequest)
		if err != nil || bytesWritten < len(announceRequest) {
			return bencode.Dictionary{}, err
		}

		// Step four. We read the response from server
		/*
			Offset	   Size	        Name	         Value

			0	         4	        action	         1
			4	         4	        transaction id	 same like the transaction id sent be the announce request
			8	         4	        interval	     seconds to wait till next announce
			12	         4	        leechers	     amount of leechers in swarm
			16	         4	        seeders	         amount of seeders in swarm
			20 + 6 * n	 4	        IPv4	         IP of peer
			24 + 6 * n	 2	        port	         TCP port of client
		*/

		// UDP connection doesnt allow me to read buffered, in golang it seems, so i try to read in a biiiiig buffer
		// and which can hold exactly a maximum of 10000 Peers
		const MAX_PEERS = 10000
		bigBuffer := make([]byte, 5*4+MAX_PEERS*6)
		bytesRead, err := udpConnection.Read(bigBuffer)
		bigBuffer = bigBuffer[0:bytesRead]
		firstBytes := bigBuffer[0:20]

		if err != nil {
			return bencode.Dictionary{}, err
		}

		// At this point we have all we need so we create the dictionary from scratch.

		peersAction := binary.BigEndian.Uint32(firstBytes[0:4])
		peersTransactionID := binary.BigEndian.Uint32(firstBytes[4:8])
		peersAnnounceInterval := binary.BigEndian.Uint32(firstBytes[8:12])
		peersLeechers := binary.BigEndian.Uint32(firstBytes[12:16])
		peersSeeders := binary.BigEndian.Uint32(firstBytes[16:20])

		if peersTransactionID != 1000 || peersAction != 1 {
			return bencode.Dictionary{}, errors.New("Incorrect transaction ID or action")
		}

		bigDictionary := new(bencode.Dictionary)
		bigDictionary.Values = make(map[bencode.String]bencode.Bencoder)

		announceInterval := new(bencode.Number)
		announceInterval.Value = int64(peersAnnounceInterval)
		bigDictionary.Values[bencode.String{Value: "interval"}] = announceInterval

		minAnnounceInterval := new(bencode.Number)
		minAnnounceInterval.Value = announceInterval.Value / 2
		bigDictionary.Values[bencode.String{Value: "min interval"}] = minAnnounceInterval

		complete := new(bencode.Number)
		complete.Value = int64(peersSeeders)
		bigDictionary.Values[bencode.String{Value: "complete"}] = complete

		incomplete := new(bencode.Number)
		incomplete.Value = int64(peersLeechers)
		bigDictionary.Values[bencode.String{Value: "incomplete"}] = incomplete

		peersList := new(bencode.String)
		peersList.Value = string(bigBuffer[20:])
		bigDictionary.Values[bencode.String{Value: "peers"}] = peersList

		perfectDictionary, err := GetPeers(bigDictionary)

		if err != nil {
			return bencode.Dictionary{}, errors.New("Malformed dictionary")
		}
		return perfectDictionary, nil
	}
	return bencode.Dictionary{}, errors.New("No known protocol")
}

// RequestPeers encodes an URL, making a request to announcer then
// returns the peers as a list.
func (tracker Tracker) RequestPeers(bytesUploaded int64, bytesDownloaded int64, bytesLeft int64, event int) TrackerResponse {

	trackerResponse := TrackerResponse{
		FailureReason:  "none",
		WarningMessage: "none",
		Interval:       0,
		MinInterval:    0,
		TrackerID:      "",
		Complete:       0,
		Incomplete:     0,
		Peers:          []peer.Peer{},
		AnnounceUrl:    tracker.AnnounceUrl,
	}

	peerId := tracker.PeerId

	// We create the URL like this :
	// announcer?peer_id= & info_hash= & port= & uploaded= & downloaded= & left= & event=
	// The uploaded , downloaded and left should always be , but are not necesary

	data, err := readPeersFromAnnouncer(tracker.AnnounceUrl, peerId, string(tracker.TorrentInfo.InfoHash), tracker.Port, bytesUploaded, bytesDownloaded, bytesLeft, event)
	if err != nil {
		trackerResponse.FailureReason = "I/O Timeout"
		return trackerResponse
	}

	responseDictionary, responseIsDictionary := data.(*bencode.Dictionary)

	if responseIsDictionary {

		// If the response is correct , then we parse it.

		peers, peersIsList := responseDictionary.Values[bencode.String{Value: "peers"}].(*bencode.List)

		if peersIsList {
			// At this point we have the peers as a list.

			peersList := make([]peer.Peer, 0)
			for _, peerEntry := range peers.Values {
				peerData, peerDataIsDictionary := peerEntry.(*bencode.Dictionary)
				if peerDataIsDictionary {
					ip, ipIsString := peerData.Values[bencode.String{Value: "ip"}].(*bencode.String)
					port, portIsNumber := peerData.Values[bencode.String{Value: "port"}].(*bencode.Number)
					peerId, peerIdIsString := peerData.Values[bencode.String{Value: "peer id"}].(*bencode.String)
					if ipIsString && portIsNumber && peerIdIsString {

						newPeer := peer.New(tracker.TorrentInfo, tracker.PeerId, ip.Value, int(port.Value))
						newPeer.RemotePeerId = peerId.Value
						peersList = append(peersList, newPeer)
					}
				}
			}
			trackerResponse.Peers = peersList
		}

		failureReason, failureReasonIsString := responseDictionary.Values[bencode.String{Value: "failure reason"}].(*bencode.String)
		if failureReasonIsString {
			trackerResponse.FailureReason = failureReason.Value
		}

		warning, warningIsString := responseDictionary.Values[bencode.String{Value: "warning message"}].(*bencode.String)
		if warningIsString {
			trackerResponse.WarningMessage = warning.Value
		}

		interval, intervalIsNumber := responseDictionary.Values[bencode.String{Value: "interval"}].(*bencode.Number)
		if intervalIsNumber {
			trackerResponse.Interval = interval.Value
		}

		minInterval, minIntervalIsNumber := responseDictionary.Values[bencode.String{Value: "min interval"}].(*bencode.Number)
		if minIntervalIsNumber {
			trackerResponse.MinInterval = minInterval.Value
		} else {
			trackerResponse.MinInterval = trackerResponse.Interval / 2
		}

		trackerID, trackerIDisString := responseDictionary.Values[bencode.String{Value: "tracker id"}].(*bencode.String)
		if trackerIDisString {
			trackerResponse.TrackerID = trackerID.Value
		}

		complete, completeIsNumber := responseDictionary.Values[bencode.String{Value: "complete"}].(*bencode.Number)
		if completeIsNumber {
			trackerResponse.Complete = complete.Value
		}

		incomplete, incompleteIsNumber := responseDictionary.Values[bencode.String{Value: "incomplete"}].(*bencode.Number)
		if incompleteIsNumber {
			trackerResponse.Incomplete = incomplete.Value
		}

	} else {
		trackerResponse.FailureReason = "Malformed response from tracker"
	}
	return trackerResponse
}

func (resp TrackerResponse) GetInfo() string {
	info := fmt.Sprintf("------\n")
	info += fmt.Sprintf("Announce url : %s\n", resp.AnnounceUrl)
	info += fmt.Sprintf("Failure reason : %s\n", resp.FailureReason)
	info += fmt.Sprintf("Warning message : %s\n", resp.WarningMessage)
	info += fmt.Sprintf("Interval : %d seconds\n", resp.Interval)
	info += fmt.Sprintf("Min interval : %d seconds\n", resp.MinInterval)
	info += fmt.Sprintf("Tracker id : %s\n", resp.TrackerID)
	info += fmt.Sprintf("Num peers : %d\n", len(resp.Peers))
	info += fmt.Sprintf("Seeders : %d\n", resp.Complete)
	info += fmt.Sprintf("Leechers : %d\n", resp.Incomplete)
	info += fmt.Sprintf("------\n")
	return info
}

// New returns a Tracker type with given parameters
func New(announceUrl string, info *torrent_info.TorrentInfo, port int, peerId string) Tracker {
	tracker := Tracker{
		AnnounceUrl: announceUrl,
		TorrentInfo: info,
		PeerId:      peerId,
		Port:        port,
	}
	return tracker
}
