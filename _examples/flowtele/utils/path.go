package utils

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	sd "github.com/scionproto/scion/go/lib/sciond"

	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/common"
	"github.com/scionproto/scion/go/lib/snet"
)

type ScionPathDescription struct {
	IAList []addr.IA
	IDList []common.IFIDType
}

func (spd *ScionPathDescription) IsEmpty() bool {
	return (spd.IAList == nil && spd.IDList == nil) || len(spd.IAList) == 0 && len(spd.IDList) == 0
}

func (spd *ScionPathDescription) Set(input string) error {
	if input == "" {
		spd.IAList = make([]addr.IA, 0)
		spd.IDList = make([]common.IFIDType, 0)
		return nil
	}
	isdasidList := strings.Split(input, ">")
	spd.IAList = make([]addr.IA, 2*(len(isdasidList)-1))
	spd.IDList = make([]common.IFIDType, 2*(len(isdasidList)-1))
	reFirst := regexp.MustCompile(`^([^ ]*) (\d+)$`)
	reIntermediate := regexp.MustCompile(`^(\d+) ([^ ]*) (\d+)$`)
	reLast := regexp.MustCompile(`^(\d+) ([^ ]*)$`)
	if len(isdasidList) < 2 {
		return fmt.Errorf("Cannot parse path of length %d", len(isdasidList))
	}
	index := 0
	for i, iaidString := range isdasidList {
		var elements []string
		var inId, outId, iaString string
		switch i {
		case 0:
			elements = reFirst.FindStringSubmatch(iaidString)
			iaString = elements[1]
			outId = elements[2]
		case len(isdasidList) - 1:
			elements = reLast.FindStringSubmatch(iaidString)
			inId = elements[1]
			iaString = elements[2]
		default:
			elements = reIntermediate.FindStringSubmatch(iaidString)
			inId = elements[1]
			iaString = elements[2]
			outId = elements[3]
		}
		ia, err := addr.IAFromString(iaString)
		if err != nil {
			return err
		}
		if inId != "" {
			spd.IAList[index] = ia
			err = spd.IDList[index].UnmarshalText([]byte(inId))
			index++
		}
		if outId != "" {
			spd.IAList[index] = ia
			err = spd.IDList[index].UnmarshalText([]byte(outId))
			index++
		}
	}
	return nil
}

func (spd *ScionPathDescription) String() string {
	if len(spd.IAList) < 2 {
		return "<Empty SCION path description>"
	}
	var sb strings.Builder
	for i := 0; i < len(spd.IAList); i++ {
		if i%2 == 0 {
			sb.WriteString(spd.IAList[i].String())
			sb.WriteString(" ")
			sb.WriteString(spd.IDList[i].String())
		} else {
			sb.WriteString(">")
			sb.WriteString(spd.IDList[i].String())
			sb.WriteString(" ")
		}
	}
	sb.WriteString(spd.IAList[len(spd.IAList)-1].String())
	return sb.String()
}

func (spd *ScionPathDescription) IsEqual(other *ScionPathDescription) bool {
	if len(spd.IAList) != len(other.IAList) {
		return false
	}
	for i, isdas := range spd.IAList {
		if isdas != other.IAList[i] || spd.IDList[i] != other.IDList[i] {
			return false
		}
	}
	return true
}

func NewScionPathDescription(p snet.Path) *ScionPathDescription {
	var spd ScionPathDescription
	spd.IAList = make([]addr.IA, len(p.Metadata().Interfaces))
	spd.IDList = make([]common.IFIDType, len(p.Metadata().Interfaces))
	for i, ifs := range p.Metadata().Interfaces {
		spd.IAList[i] = ifs.IA
		spd.IDList[i] = ifs.ID
	}
	return &spd
}

func ReadPaths(pathsFile string) ([]*ScionPathDescription, error) {
	f, err := os.Open(pathsFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	spds := make([]*ScionPathDescription, 0)
	for scanner.Scan() {
		var spd ScionPathDescription
		spd.Set(scanner.Text())
		spds = append(spds, &spd)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return spds, nil
}

func FetchPaths(sciondAddr string, localIA, remoteIA addr.IA) ([]snet.Path, error) {
	sdConn, err := GetSciondService(sciondAddr).Connect(context.Background())
	if err != nil {
		return nil, fmt.Errorf("Unable to initialize SCION network: %s", err)
	}

	paths, err := sdConn.Paths(context.Background(), remoteIA, localIA, sd.PathReqFlags{})
	if err != nil {
		return nil, fmt.Errorf("Failed to lookup paths: %s", err)
	}
	return paths, nil
}

func FetchPath(pathDescription *ScionPathDescription, sciondAddr string, localIA, remoteIA addr.IA) (snet.Path, error) {
	paths, err := FetchPaths(sciondAddr, localIA, remoteIA)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if pathDescription.IsEqual(NewScionPathDescription(path)) {
			return path, nil
		}
	}
	return nil, fmt.Errorf("No matching path (%v) was found in %v", pathDescription, paths)
}
