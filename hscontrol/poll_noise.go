package hscontrol

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

const (
	MinimumCapVersion tailcfg.CapabilityVersion = 36
)

// NoisePollNetMapHandler takes care of /machine/:id/map using the Noise protocol
//
// This is the busiest endpoint, as it keeps the HTTP long poll that updates
// the clients when something in the network changes.
//
// The clients POST stuff like HostInfo and their Endpoints here, but
// only after their first request (marked with the ReadOnly field).
//
// At this moment the updates are sent in a quite horrendous way, but they kinda work.
func (ns *noiseServer) NoisePollNetMapHandler(
	writer http.ResponseWriter,
	req *http.Request,
) {
	log.Trace().
		Str("handler", "NoisePollNetMap").
		Msg("PollNetMapHandler called")

	log.Trace().
		Any("headers", req.Header).
		Caller().
		Msg("Headers")

	body, _ := io.ReadAll(req.Body)

	mapRequest := tailcfg.MapRequest{}
	if err := json.Unmarshal(body, &mapRequest); err != nil {
		log.Error().
			Caller().
			Err(err).
			Msg("Cannot parse MapRequest")
		http.Error(writer, "Internal error", http.StatusInternalServerError)

		return
	}

	// Reject unsupported versions
	if mapRequest.Version < MinimumCapVersion {
		log.Info().
			Caller().
			Int("min_version", int(MinimumCapVersion)).
			Int("client_version", int(mapRequest.Version)).
			Msg("unsupported client connected")
		http.Error(writer, "Internal error", http.StatusBadRequest)

		return
	}

	ns.nodeKey = mapRequest.NodeKey

	node, err := ns.headscale.db.GetNodeByAnyKey(
		ns.conn.Peer(),
		mapRequest.NodeKey,
		key.NodePublic{},
	)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warn().
				Str("handler", "NoisePollNetMap").
				Msgf("Ignoring request, cannot find node with key %s", mapRequest.NodeKey.String())
			http.Error(writer, "Internal error", http.StatusNotFound)

			return
		}
		log.Error().
			Str("handler", "NoisePollNetMap").
			Msgf("Failed to fetch node from the database with node key: %s", mapRequest.NodeKey.String())
		http.Error(writer, "Internal error", http.StatusInternalServerError)

		return
	}
	log.Debug().
		Str("handler", "NoisePollNetMap").
		Str("node", node.Hostname).
		Int("cap_ver", int(mapRequest.Version)).
		Msg("A node sending a MapRequest with Noise protocol")

	ns.headscale.handlePoll(writer, req.Context(), node, mapRequest)
}
