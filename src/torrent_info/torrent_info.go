package torrent_info

import (
	"bencode"
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
	PieceLength   int64
	Pieces        string
	Private       int64
}

type TorrentInfo struct {
	FileInformations InfoDictionary
	AnnounceUrl      string
	AnnounceList     []string
	CreationDate     int64
	Comment          string
	CreatedBy        string
	Encoding         string
}

const DICTIONARY_TYPE = 4
const NUMBER_TYPE = 2
const LIST_TYPE = 3
const STRING_TYPE = 1

func checkType(decoded bencode.Bencoder, typeId int) bool {

	// 1 - String
	// 2 - Number
	// 3 - List
	// 4 - Dictionary

	switch decoded.(type) {
	case bencode.String:
		return (typeId == STRING_TYPE)
	case bencode.Number:
		return (typeId == NUMBER_TYPE)
	case bencode.List:
		return (typeId == LIST_TYPE)
	case bencode.Dictionary:
		return (typeId == DICTIONARY_TYPE)
	default:
		return false
	}
}

func getFileInformationFromBencoder(decoded bencode.Bencoder, output *TorrentInfo) {

	if !checkType(decoded, DICTIONARY_TYPE) {
		return
	}

	dictionary := decoded.(bencode.Dictionary)

	oneFile := SingleFileInfo{}

	for keys, values := range dictionary.Values {

		keyString := string((*keys).Value)
		switch keyString {
		case "path":
			// This should be a list of string paths

			if !checkType(*values, LIST_TYPE) {
				continue
			}

			var pathString string = ""
			pathsList := (*values).(bencode.List)
			for _, pathPart := range pathsList.Values {
				if checkType(*pathPart, STRING_TYPE) {
					pathString += "/" + string((*pathPart).(bencode.String).Value)
				}
			}
			oneFile.Name = pathString
		case "length":
			if checkType(*values, NUMBER_TYPE) {
				oneFile.Length = (*values).(bencode.Number).Value
			}
		case "md5sum":
			if checkType(*values, STRING_TYPE) {
				oneFile.Md5sum = string((*values).(bencode.String).Value)
			}
		default:
		}
	}

	output.FileInformations.Files = append(output.FileInformations.Files, oneFile)
}

func getInfoDictionaryFromBencoder(decoded bencode.Bencoder, output *TorrentInfo) {

	if !checkType(decoded, DICTIONARY_TYPE) {
		return
	}

	dictionary := decoded.(bencode.Dictionary)

	for keys, values := range dictionary.Values {

		keyString := string((*keys).Value)
		switch keyString {
		case "piece length":
			if checkType(*values, NUMBER_TYPE) {
				output.FileInformations.PieceLength = (*values).(bencode.Number).Value
			}
		case "pieces":
			if checkType(*values, STRING_TYPE) {
				output.FileInformations.Pieces = string((*values).(bencode.String).Value)
			}
		case "private":
			if checkType(*values, NUMBER_TYPE) {
				output.FileInformations.Private = (*values).(bencode.Number).Value
			}
		}
	}

	output.FileInformations.MultipleFiles = false

	// Check if there are multiple files or not
	for keys, _ := range dictionary.Values {

		keyString := string((*keys).Value)
		if keyString == "files" {
			output.FileInformations.MultipleFiles = true
		}
	}

	if output.FileInformations.MultipleFiles {
		//We have two or more files

		for keys, values := range dictionary.Values {

			keyString := string((*keys).Value)
			switch keyString {
			case "name":
				if checkType(*values, STRING_TYPE) {
					output.FileInformations.RootPath = string((*values).(bencode.String).Value)
				}
			case "files":
				// We should have a list of dictionaries
				if !checkType(*values, LIST_TYPE) {
					continue
				}
				fileList := (*values).(bencode.List)
				for _, oneFileBencoded := range fileList.Values {
					getFileInformationFromBencoder(*oneFileBencoded, output)
				}

			default:
			}
		}

	} else {

		// We have only one file
		oneFile := SingleFileInfo{}
		for keys, values := range dictionary.Values {

			keyString := string((*keys).Value)
			switch keyString {
			case "name":
				if checkType(*values, STRING_TYPE) {
					output.FileInformations.RootPath = string((*values).(bencode.String).Value)
					oneFile.Name = output.FileInformations.RootPath
				}
			case "length":
				if checkType(*values, NUMBER_TYPE) {
					oneFile.Length = (*values).(bencode.Number).Value
				}
			case "md5sum":
				if checkType(*values, STRING_TYPE) {
					oneFile.Md5sum = string((*values).(bencode.String).Value)
				}
			default:

			}
		}

		output.FileInformations.Files = append(output.FileInformations.Files, oneFile)

	}

}

func GetInfoFromBencoder(decoded bencode.Bencoder) TorrentInfo {

	info := TorrentInfo{}

	// check is bencoder is a bencode.Dictionary
	if !checkType(decoded, DICTIONARY_TYPE) {
		return info
	}

	dictionary := decoded.(bencode.Dictionary)

	for keys, values := range dictionary.Values {
		keyString := string((*keys).Value)

		switch keyString {
		case "announce":
			if checkType(*values, STRING_TYPE) {
				info.AnnounceUrl = string((*values).(bencode.String).Value)
			}
		case "comment":
			if checkType(*values, STRING_TYPE) {
				info.Comment = string((*values).(bencode.String).Value)
			}
		case "created by":
			if checkType(*values, STRING_TYPE) {
				info.CreatedBy = string((*values).(bencode.String).Value)
			}
		case "creation date":
			if checkType(*values, NUMBER_TYPE) {
				info.CreationDate = (*values).(bencode.Number).Value
			}
		case "encoding":
			if checkType(*values, STRING_TYPE) {
				info.Encoding = string((*values).(bencode.String).Value)
			}
		case "announce-list":

			// It should be a list of list of strings but we check each time we convert the interface
			if !checkType(*values, LIST_TYPE) {
				continue
			}
			announceListStrings := (*values).(bencode.List)
			for _, listBencoder := range announceListStrings.Values {

				if !checkType(*listBencoder, LIST_TYPE) {
					continue
				}

				realList := (*listBencoder).(bencode.List)

				for _, strings := range realList.Values {
					if checkType(*strings, STRING_TYPE) {
						realString := (*strings).(bencode.String)
						info.AnnounceList = append(info.AnnounceList, string(realString.Value))
					}
				}
			}
		case "info":
			getInfoDictionaryFromBencoder(*values, &info)
		default:

		}
	}
	return info
}
