package torrent_info

import (
    "bencode"
    "errors"
    "fmt"
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

func (torrentInfo TorrentInfo) Description() string {
    description :=
        fmt.Sprintln("-----") +
            fmt.Sprintln("Announce url :", torrentInfo.AnnounceUrl) +
            fmt.Sprintln("Announce additional list :", torrentInfo.AnnounceList) +
            fmt.Sprintln("Creation date :", torrentInfo.CreationDate) +
            fmt.Sprintln("Comment :", torrentInfo.Comment) +
            fmt.Sprintln("Created by :", torrentInfo.CreatedBy) +
            fmt.Sprintln("Encoding :", torrentInfo.Encoding) +
            fmt.Sprintln("Piece Length :", torrentInfo.FileInformations.PieceLength) +
            fmt.Sprintln("Private :", torrentInfo.FileInformations.Private) +
            fmt.Sprintln("Simple Single file torrent? :", !torrentInfo.FileInformations.MultipleFiles) +
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

func getInfoDictionaryFromBencoder(decoded bencode.Bencoder, output *TorrentInfo) error {

    if _, isDictionary := decoded.(*bencode.Dictionary); !isDictionary {
        return errors.New("Malformed torrent file")
    }

    dictionary := decoded.(*bencode.Dictionary)

    for key, value := range dictionary.Values {
        switch key.Value {
        case "piece length":
            if data, isNumber := value.(*bencode.Number); isNumber {
                output.FileInformations.PieceLength = data.Value
            }
        case "pieces":
            if data, isString := value.(*bencode.String); isString {
                output.FileInformations.Pieces = data.Value
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