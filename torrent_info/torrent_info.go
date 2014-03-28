// Package torrent_info gathers all the info from a given Bencoder Object
// All you need to know is the GetInfoFromBencoder() function
package torrent_info

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"github.com/bbpcr/Yomato/bencode"
)

type SingleFileInfo struct {
	Name   string
	Length int64
	Md5sum string
}

type InfoDictionary struct {
	RootPath      string
	Files         []SingleFileInfo
	MultipleFiles bool
	TotalLength   int64
	PieceLength   int64
	PieceCount    int64
	Pieces        []byte
	Private       int64
}

// TorrentInfo stores useful information about a Torrent file
type TorrentInfo struct {
	FileInformations InfoDictionary
	AnnounceUrl      string
	AnnounceList     []string
	CreationDate     int64
	Comment          string
	CreatedBy        string
	Encoding         string
	InfoHash         []byte
}

// Description prints out fields of a TorrentInfo object.
func (torrentInfo TorrentInfo) Description() string {
	description :=
		fmt.Sprintln("-----") +
			fmt.Sprintln("Announce url :", torrentInfo.AnnounceUrl) +
			fmt.Sprintln("Announce additional list :", torrentInfo.AnnounceList) +
			fmt.Sprintln("Creation date :", torrentInfo.CreationDate) +
			fmt.Sprintln("Comment :", torrentInfo.Comment) +
			fmt.Sprintln("Created by :", torrentInfo.CreatedBy) +
			fmt.Sprintln("Encoding :", torrentInfo.Encoding) +
			fmt.Sprintln("Pieces : ", torrentInfo.FileInformations.PieceCount) +
			fmt.Sprintln("Piece Length :", torrentInfo.FileInformations.PieceLength) +
			fmt.Sprintln("Total Length :", torrentInfo.FileInformations.TotalLength) +
			fmt.Sprintln("Private :", torrentInfo.FileInformations.Private) +
			fmt.Sprintln("Simple Single file torrent? :", !torrentInfo.FileInformations.MultipleFiles) +
			fmt.Sprintln("Info Hash :", string(torrentInfo.InfoHash)) +
			fmt.Sprintln("File name / root name :", torrentInfo.FileInformations.RootPath) +
			"\n"

	for index, fileInfo := range torrentInfo.FileInformations.Files {
		description +=
			fmt.Sprintln("    File #", index, "-------") +
				fmt.Sprintln("    Name : ", fileInfo.Name) +
				fmt.Sprintln("    Size : ", fileInfo.Length) +
				fmt.Sprintln("    Md5sum (not always present) : ", fileInfo.Md5sum)
	}

	return description + fmt.Sprintln("-----")
}

// getFileInformationFromBencoder overrides 'Files' field of 'output' TorrentInfo pointer with given Bencoder structure.
func getFileInformationFromBencoder(decoded bencode.Bencoder, output *TorrentInfo) error {

	if _, isDictionary := decoded.(*bencode.Dictionary); !isDictionary {
		return errors.New("Malformed torrent file")
	}

	dictionary := decoded.(*bencode.Dictionary)

	oneFile := SingleFileInfo{}

	for key, value := range dictionary.Values {
		switch key.Value {
		case "path":
			// This should be a list of string paths

			var pathString string = ""
			if pathsList, isList := value.(*bencode.List); isList {
				for _, pathPart := range pathsList.Values {
					if data, isString := pathPart.(*bencode.String); isString {
						pathString += "/" + data.Value
					}
				}
			}
			oneFile.Name = pathString
		case "length":
			if data, isNumber := value.(*bencode.Number); isNumber {
				oneFile.Length = data.Value
				output.FileInformations.TotalLength += oneFile.Length
			}
		case "md5sum":
			if data, isString := value.(*bencode.String); isString {
				oneFile.Md5sum = data.Value
			}
		default:
		}
	}

	output.FileInformations.Files = append(output.FileInformations.Files, oneFile)
	return nil
}

// getInfoDictionaryFromBencoder overrides 'FileInformations' field of 'output' TorrentInfo pointer with given Bencoder structure.
func getInfoDictionaryFromBencoder(decoded bencode.Bencoder, output *TorrentInfo) error {

	if _, isDictionary := decoded.(*bencode.Dictionary); !isDictionary {
		return errors.New("Malformed torrent file")
	}

	dictionary := decoded.(*bencode.Dictionary)

	// create the info hash from tracker communication
	hash := sha1.New()
	hash.Write(dictionary.Encode())
	output.InfoHash = hash.Sum(nil)

	for key, value := range dictionary.Values {
		switch key.Value {
		case "piece length":
			if data, isNumber := value.(*bencode.Number); isNumber {
				output.FileInformations.PieceLength = data.Value
			}
		case "pieces":
			if data, isString := value.(*bencode.String); isString {
				output.FileInformations.Pieces = []byte(data.Value)
				piecesLength := len(output.FileInformations.Pieces)
				if piecesLength%20 == 0 {
					output.FileInformations.PieceCount = int64(piecesLength) / 20
				} else {
					return errors.New("Pieces is not a multiple of 20!")
				}
			}
		case "private":
			if data, isNumber := value.(*bencode.Number); isNumber {
				output.FileInformations.Private = data.Value
			}
		}
	}

	output.FileInformations.MultipleFiles = false

	// Check if there are multiple files or not
	for key, _ := range dictionary.Values {
		if key.Value == "files" {
			output.FileInformations.MultipleFiles = true
			break
		}
	}

	if output.FileInformations.MultipleFiles {

		output.FileInformations.TotalLength = 0

		//We have two or more files
		for key, value := range dictionary.Values {
			switch key.Value {
			case "name":
				if data, isString := value.(*bencode.String); isString {
					output.FileInformations.RootPath = data.Value
				}
			case "files":
				if fileList, isList := value.(*bencode.List); isList {
					for _, oneFileBencoded := range fileList.Values {
						if err := getFileInformationFromBencoder(
							oneFileBencoded, output); err != nil {
							return err
						}
					}
				}

			default:
			}
		}
	} else {

		// We have only one file
		oneFile := SingleFileInfo{}
		for key, value := range dictionary.Values {
			switch key.Value {
			case "name":
				if data, isString := value.(*bencode.String); isString {
					output.FileInformations.RootPath = data.Value
					oneFile.Name = data.Value
				}
			case "length":
				if data, isNumber := value.(*bencode.Number); isNumber {
					oneFile.Length = data.Value
					output.FileInformations.TotalLength = oneFile.Length
				}
			case "md5sum":
				if data, isString := value.(*bencode.String); isString {
					oneFile.Md5sum = data.Value
				}
			default:

			}
		}

		output.FileInformations.Files = append(output.FileInformations.Files, oneFile)

	}
	return nil
}

// GetInfoFromBencoder tries to get all the information from the Bencoder and returns a TorrentInfo pointer,
// hopefully filling all the fields of a TorrentInfo structure.
func GetInfoFromBencoder(decoded bencode.Bencoder) (*TorrentInfo, error) {

	info := &TorrentInfo{}

	// check is bencoder is a bencode.Dictionary
	if _, isDictionary := decoded.(*bencode.Dictionary); !isDictionary {
		return info, errors.New("Malformed torrent file")
	}

	dictionary := decoded.(*bencode.Dictionary)

	// let's not throw an error of the type does not match for the simple ones
	// the info field however is very important
	for key, value := range dictionary.Values {
		switch key.Value {
		case "announce":
			if data, isString := value.(*bencode.String); isString {
				info.AnnounceUrl = data.Value
			}
		case "comment":
			if data, isString := value.(*bencode.String); isString {
				info.Comment = data.Value
			}
		case "created by":
			if data, isString := value.(*bencode.String); isString {
				info.CreatedBy = data.Value
			}
		case "creation date":
			if data, isNumber := value.(*bencode.Number); isNumber {
				info.CreationDate = data.Value
			}
		case "encoding":
			if data, isString := value.(*bencode.String); isString {
				info.Encoding = data.Value
			}
		case "announce-list":

			// It should be a list of list of strings but we check each time we convert the interface
			if announceList, isList := value.(*bencode.List); isList {
				for _, listBencoder := range announceList.Values {
					if announce, isList := listBencoder.(*bencode.List); isList {
						for _, str := range announce.Values {
							if realString, isString := str.(*bencode.String); isString {
								info.AnnounceList = append(info.AnnounceList, realString.Value)
							}
						}
					}
				}
			}
		case "info":
			if err := getInfoDictionaryFromBencoder(value, info); err != nil {
				return info, err
			}
		default:

		}
	}
	return info, nil
}
