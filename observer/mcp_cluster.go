package observer

import (
	"fmt"
	"net/http"
	"slices"
	"time"

	"ergo.services/ergo/gen"
)

const clusterURI = mcpScheme + uriWordCluster

func (w *postWorker) mcpCluster(writer http.ResponseWriter, request *http.Request, envelope mcpEnvelope) {
	identity, _ := identityOf(request)

	target := gen.ProcessID{Name: clusterName, Node: w.Node().Name()}
	result, err := w.CallWithTimeout(target, RequestClusterInfo{}, mcpReadTimeout)
	if err != nil {
		w.mcpFail(writer, request, envelope.ID, mcpInternalError, err.Error(),
			map[string]any{"uri": clusterURI})
		return
	}

	info, ok := result.(ClusterInfo)
	if ok == false {
		w.mcpFail(writer, request, envelope.ID, mcpInternalError,
			fmt.Sprintf("unexpected answer %T", result), nil)
		return
	}

	rendered, err := mcpRender(narrowCluster(info, identity.Ceiling))
	if err != nil {
		w.mcpFail(writer, request, envelope.ID, mcpInternalError, "the map is not renderable", nil)
		return
	}

	writeJSON(writer, request, http.StatusOK, mcpOK(envelope.ID, mcpContents{
		ResultType: mcpResultComplete,
		Contents: []mcpResourceContent{{
			URI:      clusterURI,
			MimeType: rendered.MimeType,
			Text:     rendered.Text,
		}},
		TTLMs:      clusterTTL(info.WatchPeriod),
		CacheScope: mcpCachePrivate,
		Meta: &mcpReadMeta{
			ServerInfo:   mcpIdentity(),
			LastModified: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}))
}

func clusterTTL(period time.Duration) int {
	if period <= 0 {
		return ttlReading
	}
	return int(period.Milliseconds())
}

func narrowCluster(info ClusterInfo, ceiling Ceiling) ClusterInfo {
	if ceiling.Nodes == nil {
		return info
	}

	out := info
	out.Nodes = slices.DeleteFunc(slices.Clone(info.Nodes), func(node ClusterNodeInfo) bool {
		return ceiling.AllowsNode(node.Info.Name) == false
	})
	out.Offline = slices.DeleteFunc(slices.Clone(info.Offline), func(node OfflineNode) bool {
		return ceiling.AllowsNode(node.Node) == false
	})

	for i := range out.Offline {
		out.Offline[i].Peers = slices.DeleteFunc(slices.Clone(out.Offline[i].Peers),
			func(peer gen.Atom) bool { return ceiling.AllowsNode(peer) == false })
	}

	out.Complete = false
	out.Note = "narrowed by the ceiling"
	out.Watched = len(out.Nodes)
	return out
}
