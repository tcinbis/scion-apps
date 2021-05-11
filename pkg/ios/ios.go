package ios

import (
	"C"
	"time"

	"github.com/netsec-ethz/scion-apps/pkg/pan"
)
import (
	"context"
	"errors"
	"net"

	"github.com/scionproto/scion/go/lib/addr"
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

func (p Path)Reversed() (*Path, error) {
	r, err := p.underlying.Reversed()
	if err != nil { return nil, err }
	return &Path { underlying: r }, nil
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

func (a UDPAddress) IsForeignTo(other *UDPAddress) bool {
	return !addr.IA(a.underlying.IA).Equal(addr.IA(other.underlying.IA))
}

type Connection struct {
	underlying pan.Conn
	policy *pathPolicy
	selector *defaultSelector
}

type Listener struct {
	underlying pan.UDPListener
}

func DialUDP(destination *UDPAddress, policyFilter PathPolicyFilter) (*Connection, error) {
	policy := &pathPolicy { filter: policyFilter }
	sel := &defaultSelector{}
	c, err := pan.DialUDP(context.Background(), nil, destination.underlying, policy, sel)
	if err != nil { return nil, err }
	return &Connection{ underlying: c, policy: policy, selector: sel }, nil
}

func ListenUDP(port int) (*Listener, error) {
	l, err := pan.ListenUDP(context.Background(), &net.UDPAddr{ Port: port }, nil)
	if err != nil { return nil, err }
	return &Listener{ underlying: l }, nil
}

func (l Listener) MakeConnectionToRemote(remote *UDPAddress, policyFilter PathPolicyFilter) (*Connection, error) {
	policy := &pathPolicy { filter: policyFilter }
	sel := &defaultSelector{}
	c, err := l.underlying.MakeConnectionToRemote(context.Background(), remote.underlying, policy, sel)
	if err != nil { return nil, err}
	return &Connection{ underlying: c, policy: policy, selector: sel }, nil
}

type ReadResult struct {
    BytesRead int
    Source *UDPAddress
	Path *Path
    Err error
}

type WriteResult struct {
    BytesWritten int
	Path *Path
    Err error
}

func (c Connection) SetPathSelectorObserver(observer SelectorObserver) {
    c.selector.observer = observer
}

func (c Connection) GetRemoteAddress() *UDPAddress {
    return &UDPAddress{ underlying: c.underlying.RemoteAddr().(pan.UDPAddr) }
}

func (c Connection) GetLocalAddress() *UDPAddress {
    return &UDPAddress{ underlying: c.underlying.LocalAddr().(pan.UDPAddr) }
}

func (c Connection) Read(buffer []byte) *ReadResult {
    n, p, e := c.underlying.ReadPath(buffer)
	if e != nil {
		// wrap the error to curcumvent idiotic go error: panic: runtime error: hash of unhashable type serrors.basicError
		return &ReadResult{0, nil, nil, errors.New(e.Error())}
	}
    var pp *Path
	if p != nil {
		 pp = &Path{underlying: p}
	} else {
		pp = nil
	}
    return &ReadResult{n, c.GetRemoteAddress(), pp, e}
}

func (c Connection) WritePath(buffer []byte, path *Path) *WriteResult {
    w, e := c.underlying.WritePath(path.underlying, buffer)
	if e != nil {
		// wrap the error to curcumvent idiotic go error: panic: runtime error: hash of unhashable type serrors.basicError
		return &WriteResult{0, nil, errors.New(e.Error())}
	}
	return &WriteResult { BytesWritten: w, Path: path, Err: e }
}

func (c Connection) Write(buffer []byte) *WriteResult {
    p, w, e := c.underlying.WriteGetPath(buffer)
	if e != nil {
		// wrap the error to curcumvent idiotic go error: panic: runtime error: hash of unhashable type serrors.basicError
		return &WriteResult{0, nil, errors.New(e.Error())}
	}
	var pp *Path
	if p != nil {
		 pp = &Path{underlying: p}
	} else {
		pp = nil
	}
	return &WriteResult { BytesWritten: w, Path: pp, Err: e }
}

func (l Listener) Read(buffer []byte) *ReadResult {
    n, a, p, e := l.underlying.ReadFromPath(buffer)
	if e != nil {
		// wrap the error to curcumvent idiotic go error: panic: runtime error: hash of unhashable type serrors.basicError
		return &ReadResult{0, nil, nil, errors.New(e.Error())}
	}
    var pp *Path
	if p != nil {
		 pp = &Path{underlying: p}
	} else {
		pp = nil
	}
    return &ReadResult{n, &UDPAddress{ underlying: a }, pp, e}
}

func (l Listener) GetLocalAddress() *UDPAddress {
    return &UDPAddress{ underlying: l.underlying.LocalAddr().(pan.UDPAddr) }
}

/// Forces a re-evaluation of the policy. Use when the underlying PathPolicyFilter behavior changes
func (c Connection) UpdatePolicy() {
	c.underlying.SetPolicy(c.policy)
}

func (c Connection) GetPaths() *PathCollection {
	u := c.selector.AllPaths()
	if u == nil { return nil }
	return &PathCollection{ underlying: u }
}

// func (c Connection) FixPath(path *Path) bool {
// 	if path == nil {
// 		return c.selector.FixPath(nil, false)
// 	}
// 	return c.selector.FixPath(path.underlying, false) // false here can lead to undefined behavior but we will assume that no bad values are entered... Better for performance like this
// }

// func (c Connection) IsPathFixed() bool {
// 	return c.selector.IsPathFixed()
// }

func (c Connection) GetCurrentPath() *Path {
	if c.selector.Path() == nil { return nil }
	return &Path { underlying: c.selector.Path() }
}

func (c Connection) Close() {
    c.underlying.Close()
}

func (c Listener) Close() {
    c.underlying.Close()
}
