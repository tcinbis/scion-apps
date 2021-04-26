package ios

import (
	"C"
	"time"

	"github.com/netsec-ethz/scion-apps/pkg/pan"
)
import (
	"context"
	"net"
)

// Yo dawg I heard you like wrappers, so I put a....
type Path struct {
	underlying *pan.Path
}

type PathRaw struct {
	underlying pan.ForwardingPath
}

type PathCollectionSource interface {
	GetPathAt(index int) *Path
	GetPathCount() int
}

func PathCollectionSourceToSlice(source PathCollectionSource) []*pan.Path {
	sl := make([]*pan.Path, source.GetPathCount())
	for i := 0; i < source.GetPathCount(); i++ {
		sl[i] = source.GetPathAt(i).underlying
	}
	return sl
}

// Implements PathCollectionSource
type PathCollection struct {
	underlying []*pan.Path
}

func (self *PathCollection) GetPathCount() int {
	return len(self.underlying)
}

func (self *PathCollection) GetPathAt(index int) *Path {
	return &Path { underlying: self.underlying[index] }
}

type PathPolicyFilter interface {
	Sort(paths PathCollectionSource) PathCollectionSource
}

// implements pan.PathPolicy
type pathPolicy struct {
	filter PathPolicyFilter
}

// func PathPolicyMake(filter PathPolicyFilter) *PathPolicy {
// 	return &PathPolicy { filter: filter }
// }

func (self *pathPolicy) Filter(paths []*pan.Path) []*pan.Path {
	collection := &PathCollection { underlying: paths }
	filtered := self.filter.Sort(collection)
	return PathCollectionSourceToSlice(filtered)
}

type PathMetadata struct {
    underlying *pan.PathMetadata
}

// In bytes
func (m PathMetadata)GetMTU() int32 {
    return int32(m.underlying.MTU);
} 

// In microseconds
func (m PathMetadata)GetLatencyAt(index int) int64 {
    return time.Duration(m.underlying.Latency[index]).Microseconds()
}

func (m PathMetadata)GetInterfaceIDAt(index int) int64 {
	return int64(m.underlying.Interfaces[index].IfID)
}

func (m PathMetadata)GetInterfaceIAAt(index int) string {
	return m.underlying.Interfaces[index].IA.String()
}

// In kbit/s
func (m PathMetadata)GetBandwidthAt(index int) int64 {
    return int64(m.underlying.Bandwidth[index])
}

// Related to metadata. If metadata is nil returns 0.
func (p Path)Length() int {
	if p.underlying.Metadata == nil { return 0 }
	return len(p.underlying.Metadata.Interfaces)
}

// Unix timestamp in s at UTC
func (p Path)GetExpiry() int64 {
    return p.underlying.Expiry.UTC().Unix()
}

func (p Path)GetMetadata() *PathMetadata {
    return &PathMetadata { underlying: p.underlying.Metadata }
}

func (p Path)GetRaw() *PathRaw {
	return &PathRaw { underlying: p.underlying.ForwardingPath }
}

func (p Path)GetFingerprint() string {
	return string(p.underlying.Fingerprint)
}

func (p Path)RawReversed() (*PathRaw, error) {
	r, err := p.underlying.ForwardingPath.Reversed()
	if err != nil { return nil, err }
	return &PathRaw { underlying: r }, nil
}

type UDPAddress struct {
	underlying pan.UDPAddr
}

func UDPAddressMake(str string) (*UDPAddress, error) {
	a, err :=  pan.ParseUDPAddr(str)
	if err != nil { return nil, err}
	return &UDPAddress { underlying: a }, nil
}

func (a UDPAddress) String() string {
	return a.underlying.String()
}

type Connection struct {
	underlying pan.Conn
}

type Listener struct {
	underlying pan.UDPListener
}

func DialUDP(destination *UDPAddress, policyFilter PathPolicyFilter) (*Connection, error) {
	policy := &pathPolicy { filter: policyFilter }
	c, err := pan.DialUDP(context.Background(), nil, destination.underlying, policy, nil)
	if err != nil { return nil, err }
	return &Connection{ underlying: c }, nil
}

func ListenUDP(port int) (*Listener, error) {
	l, err := pan.ListenUDP(context.Background(), &net.UDPAddr{ Port: port }, nil)
	if err != nil { return nil, err }
	return &Listener{ underlying: l }, nil
}

func (l Listener)MakeConnectionToRemote(remote UDPAddress, policyFilter PathPolicyFilter) (*Connection, error) {
	policy := &pathPolicy { filter: policyFilter }
	c, err := l.underlying.MakeConnectionToRemote(context.Background(), remote.underlying, policy, nil)
	if err != nil { return nil, err}
	return &Connection{ underlying: c }, nil
}

type ReadResult struct {
    BytesRead int
    Source *UDPAddress
    Err error
}

func (c Connection) GetRemoteAddress() *UDPAddress {
    return &UDPAddress{ underlying: c.underlying.RemoteAddr().(pan.UDPAddr) }
}

func (c Connection) GetLocalAddress() *UDPAddress {
    return &UDPAddress{ underlying: c.underlying.LocalAddr().(pan.UDPAddr) }
}

func (c Connection) Read(buffer []byte) *ReadResult {
    n, e := c.underlying.Read(buffer)
    
    return &ReadResult{n, c.GetRemoteAddress(), e}
}

func (c Connection) Write(buffer []byte) (int, error) {
    return c.underlying.Write(buffer)
}

func (l Listener) Read(buffer []byte) *ReadResult {
    n, a, e := l.underlying.ReadFrom(buffer)
    
    return &ReadResult{n, &UDPAddress{ underlying: a.(pan.UDPAddr) }, e}
}

func (c Connection) Close() {
    c.underlying.Close()
}

func (c Listener) Close() {
    c.underlying.Close()
}