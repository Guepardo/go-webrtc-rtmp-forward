package controllers

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/go-webrtc-rtmp-forward/webrtc"
)

type PeerController struct {
	BaseController
	PeerManager *webrtc.PeerManager
}

type CreatePayload struct {
	Id                      string `json:"id"`
	SessionDescriptionOffer string `json:"session_description_offer"`
	RtmpUrlWithStreamKey    string `json:"rtmp_url_with_stream_key"`
}

type ResponsePayload struct {
	SessionDescriptionOffer string `json:"session_description_offer"`
}

func (peerController *PeerController) Create(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	createPayload := CreatePayload{}

	body, err := ioutil.ReadAll(r.Body)

	if err != nil {
		panic(err.Error())
	}

	json.Unmarshal(body, &createPayload)

	serverSessionDescriptionOffer := peerController.PeerManager.HandleSessionDescriptionOffer(
		createPayload.Id,
		createPayload.SessionDescriptionOffer,
		createPayload.RtmpUrlWithStreamKey,
	)

	peerController.renderJson(w, http.StatusOK, ResponsePayload{SessionDescriptionOffer: serverSessionDescriptionOffer})
}
