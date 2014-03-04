package tracker

import (
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/bbpcr/Yomato/bencode"
	"github.com/bbpcr/Yomato/torrent_info"
	"io/ioutil"
	"net/http"
	"net/url"
)

type Tracker struct {
	MetaInfo torrent_info.TorrentInfo
	PeerId   string
	Port     int
}

func (tracker Tracker) Start() (bencode.Bencoder, error) {
	peerId := tracker.PeerId
	qs := url.Values{}
	qs.Add("peer_id", peerId)
	qs.Add("info_hash", string(tracker.MetaInfo.InfoHash))
	qs.Add("port", fmt.Sprintf("%d", tracker.Port))
	qs.Add("event", "started")
	res, err := http.Get(tracker.MetaInfo.AnnounceUrl + "?" + qs.Encode())
	if err != nil {
		return bencode.Dictionary{}, err
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return bencode.Dictionary{}, err
	}
	if res.StatusCode == 200 {
		data, _, err := bencode.Parse(body)
		if err != nil {
			return bencode.Dictionary{}, err
		}
		return data, nil
	}
	return bencode.Dictionary{}, errors.New(fmt.Sprintf("Expected 200 OK from tracker; got %s", res.Status))
}

func New(info torrent_info.TorrentInfo) Tracker {
	return Tracker{
		MetaInfo: info,
		PeerId:   createPeerId(),
		Port:     6881,
	}
}

func createPeerId() string {
	const idSize = 20
	const prefix = "YM"
	const alphanumerics = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var data = make([]byte, idSize-len(prefix))
	rand.Read(data)
	for i, b := range data {
		data[i] = alphanumerics[b%byte(len(alphanumerics))]
	}
	return prefix + string(data)
}
