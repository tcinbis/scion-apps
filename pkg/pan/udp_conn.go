package pan

import (
	"net"
)

// Conn represents a connection between exactly two hosts.
type Conn interface {
	net.Conn
	// SetPolicy allows to set the path policy for paths used by Write, at any
	// time.
	SetPolicy(policy Policy, context int64)
	// WritePath writes a message to the remote address via the given path.
	// This bypasses the path policy and selector used for Write.
	WritePath(path *Path, b []byte) (int, error)
	// Returns the path that was used
	WriteGetPath(b []byte) (*Path, int, error)
	// ReadPath reads a message and returns the (return-)path via which the
	// message was received.
	ReadPath(b []byte) (int, *Path, error)

	GetLastPath() *Path
}

type connection struct {
	*baseUDPConn

	isListener bool
	local      UDPAddr
	remote     UDPAddr
	subscriber *pathRefreshSubscriber
	Selector   Selector
	lastPath   *Path
}

func (c *connection) SetPolicy(policy Policy, context int64) {
	if c.subscriber != nil {
		c.subscriber.setPolicy(policy, context)
	}
}

func (c *connection) LocalAddr() net.Addr {
	return c.local
}

func (c *connection) RemoteAddr() net.Addr {
	return c.remote
}

func (c *connection) Write(b []byte) (int, error) {
	_, w, e := c.WriteGetPath(b)
	return w, e
}

func (c *connection) WriteGetPath(b []byte) (*Path, int, error) {
	var path *Path = nil
	if c.local.IA != c.remote.IA {
		path = c.Selector.Path()
		if path == nil {
			return nil, 0, errNoPathTo(c.remote.IA)
		}
	}
	w, e := c.WritePath(path, b)
	return path, w, e
}

func (c *connection) WritePath(path *Path, b []byte) (int, error) {
	return c.baseUDPConn.writeMsg(c.local, c.remote, path, b)
}

func (c *connection) Read(b []byte) (int, error) {
	for {
		n, remote, fwPath, err := c.baseUDPConn.readMsg(b)
		if err != nil {
			return n, err
		}
		if !remote.Equal(c.remote) {
			continue // connected! Ignore spurious packets from wrong source
		}
		go func() {
			if fwPath.IsEmpty() {
				return
			}
			fpi, err := fwPath.forwardingPathInfo()
			if err != nil {
				return
			}
			ps := pathSequence{InterfaceIDs: fpi.interfaceIDs}
			if c.lastPath != nil && c.lastPath.Fingerprint == ps.Fingerprint() {
				return
			}
			c.lastPath = &Path{
				Source:         remote.IA,
				Destination:    c.local.IA,
				ForwardingPath: fwPath,
				Metadata:       nil,
				Fingerprint:    ps.Fingerprint(),
			}
		}()

		return n, err
	}
}

func (c *connection) ReadPath(b []byte) (int, *Path, error) {
	for {
		n, remote, fwPath, err := c.baseUDPConn.readMsg(b)
		if err != nil {
			return n, nil, err
		}
		if !remote.Equal(c.remote) {
			continue // connected! Ignore spurious packets from wrong source
		}
		path, err := reversePathFromForwardingPath(c.remote.IA, c.local.IA, fwPath)
		if err != nil {
			continue // just drop the packet if there is something wrong with the path
		}
		return n, path, nil
	}
}

func (c *connection) GetLastPath() *Path {
	return c.lastPath
}

// TODO: A conn stemming from a listener should not close the listener socket
func (c *connection) Close() error {
	if c.subscriber != nil {
		_ = c.subscriber.Close()
	}

	if !c.isListener {
		return c.baseUDPConn.Close()
	}
	return nil
}
