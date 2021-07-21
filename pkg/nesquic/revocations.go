package nesquic

import (
	"context"
	"github.com/netsec-ethz/scion-apps/pkg/appnet"
	"github.com/scionproto/scion/go/lib/ctrl/path_mgmt"
	"github.com/scionproto/scion/go/lib/snet"
)

var _ snet.RevocationHandler = (*revocationHandler)(nil)

type revocationHandler struct {
	revocationQ chan *path_mgmt.RevInfo
}

func (h *revocationHandler) Revoke(_ context.Context, sRevInfo *path_mgmt.RevInfo) {
	select {
	case h.revocationQ <- sRevInfo:
		logger.Info("Enqueued revocation", "revInfo", sRevInfo)
	default:
		logger.Info("Ignoring scmp packet", "cause", "Revocation channel full.")
	}
}

// handleSCMPRevocation handles explicit revocation notification of a link on a
// path that is either being probed or actively used for the data stream.
// Returns true iff the currently active path was revoked.
func (mpq *MPQuic) handleRevocation(sRevInfo *path_mgmt.RevInfo) bool {

	// Revoke path from sciond
	_ = appnet.DefNetwork().Sciond.RevNotification(context.TODO(), sRevInfo)

	activePathRevoked := false
	if sRevInfo.Active() == nil {
		for i, pathInfo := range mpq.paths {
			if matches(pathInfo.path, snet.PathInterface{
				ID: sRevInfo.IfID,
				IA: sRevInfo.IA(),
			}) {
				pathInfo.revoked = true
				if i == mpq.active {
					activePathRevoked = true
				}
			}
		}
	} else {
		// Ignore expired revocations
		logger.Debug("Processing revocation", "action", "Ignoring expired revocation.")
	}
	return activePathRevoked
}

// matches returns true if the path contains the interface described by ia/ifID
func matches(path snet.Path, predicatePI snet.PathInterface) bool {
	for _, pi := range path.Metadata().Interfaces {
		if pi.IA.Equal(predicatePI.IA) && pi.ID == predicatePI.ID {
			return true
		}
	}
	return false
}
